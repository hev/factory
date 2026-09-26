#!/usr/bin/env python3
"""PNG of honest pixels -> SVG of merged rects, so it scales without resampling."""
import sys
from PIL import Image

src, dst = sys.argv[1], sys.argv[2]
img = Image.open(src).convert("RGBA")
w, h = img.size
px = img.load()
rects = []
for y in range(h):
    x = 0
    while x < w:
        r, g, b, a = px[x, y]
        if a == 0:
            x += 1
            continue
        run = 1
        while x + run < w and px[x + run, y] == (r, g, b, a):
            run += 1
        rects.append(f'<rect x="{x}" y="{y}" width="{run}" height="1" fill="#{r:02x}{g:02x}{b:02x}"/>')
        x += run
open(dst, "w").write(
    f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w}" height="{h}" '
    f'shape-rendering="crispEdges">\n' + "\n".join(rects) + "\n</svg>\n"
)
print(f"{dst}: {len(rects)} rects from {w}x{h}")
