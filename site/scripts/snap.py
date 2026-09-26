#!/usr/bin/env python3
"""Snap model output to a real pixel grid.

The image model draws pixel art the way it draws everything else: soft edges,
a hundred near-identical colors, and a block size that drifts across the frame.
This finds the grid the picture is *trying* to be on, samples one color per
cell, collapses the palette, and writes the result back out as honest pixels.

    snap.py IN OUT --colors 14 [--grid N] [--key-bg] [--trim]
"""
import argparse
from collections import Counter

from PIL import Image


def detect_block(pixels, length, other, horizontal=True):
    """Find the repeating cell size by looking at where the color changes."""
    diffs = []
    for i in range(length - 1):
        total = 0
        for j in range(0, other, max(1, other // 64)):
            a = pixels[i, j] if horizontal else pixels[j, i]
            b = pixels[i + 1, j] if horizontal else pixels[j, i + 1]
            total += abs(a[0] - b[0]) + abs(a[1] - b[1]) + abs(a[2] - b[2])
        diffs.append(total)
    if not diffs:
        return None
    threshold = max(diffs) * 0.18
    edges = [i for i, d in enumerate(diffs) if d > threshold]
    gaps = Counter()
    for a, b in zip(edges, edges[1:]):
        gap = b - a
        if 2 <= gap <= length // 6:
            gaps[gap] += 1
    if not gaps:
        return None
    # the mode of the gaps is the cell size; multiples of it also show up, so
    # prefer the smallest gap that explains most of the mass
    best = gaps.most_common(1)[0][0]
    for gap in sorted(gaps):
        if gaps[gap] >= gaps[best] * 0.6:
            return gap
    return best


def sample(img, cols, rows):
    """One color per cell: the most common color in the middle of the cell."""
    w, h = img.size
    px = img.load()
    out = Image.new("RGBA", (cols, rows))
    op = out.load()
    for cy in range(rows):
        for cx in range(cols):
            x0, x1 = cx * w // cols, (cx + 1) * w // cols
            y0, y1 = cy * h // rows, (cy + 1) * h // rows
            # inset so the soft edge between cells never votes
            ix = max(1, (x1 - x0) // 4)
            iy = max(1, (y1 - y0) // 4)
            votes = Counter()
            for y in range(y0 + iy, max(y0 + iy + 1, y1 - iy)):
                for x in range(x0 + ix, max(x0 + ix + 1, x1 - ix)):
                    if x < w and y < h:
                        votes[px[x, y][:3]] += 1
            op[cx, cy] = (*votes.most_common(1)[0][0], 255) if votes else (0, 0, 0, 0)
    return out


def quantize(img, colors):
    """Collapse to a fixed palette, then snap every pixel to its nearest entry."""
    rgb = img.convert("RGB")
    pal = rgb.quantize(colors=colors, method=Image.MEDIANCUT, dither=Image.NONE)
    return pal.convert("RGBA")


def key_background(img, tolerance=40):
    """Flood the outer background to transparent, starting from the corners."""
    w, h = img.size
    px = img.load()
    seeds = [(0, 0), (w - 1, 0), (0, h - 1), (w - 1, h - 1)]
    targets = [px[s][:3] for s in seeds]
    seen = set()
    stack = list(seeds)
    while stack:
        x, y = stack.pop()
        if (x, y) in seen or not (0 <= x < w and 0 <= y < h):
            continue
        seen.add((x, y))
        c = px[x, y][:3]
        if not any(sum(abs(a - b) for a, b in zip(c, t)) <= tolerance for t in targets):
            continue
        px[x, y] = (0, 0, 0, 0)
        stack.extend([(x + 1, y), (x - 1, y), (x, y + 1), (x, y - 1)])
    return img


def trim(img):
    box = img.getbbox()
    return img.crop(box) if box else img


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("src")
    ap.add_argument("out")
    ap.add_argument("--colors", type=int, default=14)
    ap.add_argument("--grid", type=int, default=0, help="force N cells across")
    ap.add_argument("--key-bg", action="store_true")
    ap.add_argument("--trim", action="store_true")
    ap.add_argument("--scales", default="1,8,16")
    args = ap.parse_args()

    img = Image.open(args.src).convert("RGB")
    w, h = img.size
    px = img.load()

    if args.grid:
        cols = args.grid
        rows = round(h * cols / w)
    else:
        bx = detect_block(px, w, h, True) or 16
        by = detect_block(px, h, w, False) or bx
        block = min(bx, by)
        cols = round(w / block)
        rows = round(h / block)
    print(f"grid: {cols}x{rows} cells from {w}x{h}")

    small = sample(img, cols, rows)
    small = quantize(small, args.colors)
    if args.key_bg:
        small = key_background(small)
    if args.trim:
        small = trim(small)
    print(f"result: {small.size[0]}x{small.size[1]}, {len(set(small.getdata()))} colors")

    for scale in [int(s) for s in args.scales.split(",")]:
        target = small if scale == 1 else small.resize(
            (small.size[0] * scale, small.size[1] * scale), Image.NEAREST
        )
        name = args.out if scale == 1 else f"{args.out.rsplit('.', 1)[0]}@{scale}x.png"
        target.save(name)
        print(f"wrote {name}")


if __name__ == "__main__":
    main()

# Provenance for what is in public/:
#   badge:     snap.py <nano-banana C4> factory-badge.png --colors 14 --key-bg
#              then tosvg.py factory-badge.png factory-badge.svg
#   the crew:  snap.py <nano-banana gaffer-2|reception-2> gaffer.png \
#              --colors 30 --key-bg --trim --scales 1,4
# Needs an interpreter with Pillow: ~/.pyenv/versions/3.12.3/bin/python3
