#!/usr/bin/env node
// The hev factory mark, drawn as pixels rather than generated.
//
// A diffusion model will not hold a pixel grid: it anti-aliases the edges and
// slides a gradient under the flats, which is exactly the two things 8-bit art
// cannot have. So the mark is authored here on a 32x32 grid with a fixed
// eight-value chrome ramp, and every output falls out of the same grid:
//
//   brand/factory-mark.svg      one <rect> per pixel — what the sticker cutter
//                               and any large print should use, since vector
//                               rects have no resampling to get wrong
//   brand/factory-mark.png      32x32, the grid itself
//   brand/factory-mark@16x.png  512x512, nearest-neighbour
//   brand/factory-mark@32x.png  1024x1024, nearest-neighbour
//   brand/favicon.png           64x64
//
// Run: node scripts/gen-logo.mjs
import { deflateSync } from "node:zlib";
import { mkdirSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const OUT = join(dirname(fileURLToPath(import.meta.url)), "..", "public", "brand");
const S = 32; // the grid is square so the mark drops into any icon slot

// Chrome is a value ramp, not a color: cool greys running from a blown
// specular white to a near-black outline, with enough steps to fake a
// reflection and few enough to still read as 8-bit.
const PALETTE = {
	".": null, // transparent
	K: "#0e1116", // outline
	1: "#ffffff", // specular
	2: "#e3ebf2", // highlight
	3: "#c0ccd8", // light
	4: "#9aa7b5", // mid
	5: "#727f8e", // dark mid
	6: "#4d5865", // shadow
	7: "#2f3742", // deep shadow / glass
};

const px = Array.from({ length: S }, () => Array(S).fill("."));
const set = (x, y, v) => {
	if (x >= 0 && x < S && y >= 0 && y < S) px[y][x] = v;
};
const get = (x, y) => (x >= 0 && x < S && y >= 0 && y < S ? px[y][x] : ".");
const rect = (x0, y0, x1, y1, v) => {
	for (let y = y0; y <= y1; y++) for (let x = x0; x <= x1; x++) set(x, y, v);
};

// --- the hall -------------------------------------------------------------
// The body carries the chrome horizon: bright sky at the top, a dark band
// across the middle, ground bounce below it, and a bright lip at the floor.
// This runs first; the roof and the stack cut into it from above.
const HALL_X0 = 1;
const HALL_X1 = 30;
const HALL_TOP = 19;
const HALL_BOTTOM = 29;
const BANDS = ["2", "3", "4", "5", "6", "5", "4", "3", "3", "2", "4"];
for (let y = HALL_TOP; y <= HALL_BOTTOM; y++) {
	rect(HALL_X0, y, HALL_X1, y, BANDS[y - HALL_TOP] ?? "4");
}

// --- the sawtooth roof ----------------------------------------------------
// Three teeth. Each is a steep glazed face on its left, lit hard, and a
// shallow roof sloping away to the right — the profile that says "factory"
// at a glance even when the whole mark is 32 pixels wide.
const ROOF_TOP = 13;
const TOOTH_W = 7;
for (let t = 0; t < 3; t++) {
	const x0 = 9 + t * TOOTH_W;
	// the glazed face, the brightest column in the mark
	rect(x0, ROOF_TOP, x0, HALL_TOP - 1, "1");
	set(x0 + 1, ROOF_TOP, "2");
	// the slope: one step down every other column
	for (let i = 1; i < TOOTH_W; i++) {
		const x = x0 + i;
		if (x > HALL_X1) break;
		const top = ROOF_TOP + i;
		set(x, top, "2"); // lit ridge
		for (let y = top + 1; y < HALL_TOP; y++) set(x, y, "3"); // shaded pitch
	}
}
// left of the first tooth the roof is a flat parapet the stack rises out of
rect(HALL_X0, HALL_TOP - 2, 8, HALL_TOP - 1, "4");
rect(HALL_X0, HALL_TOP - 2, 8, HALL_TOP - 2, "2");

// --- the smokestack -------------------------------------------------------
// A cylinder reads as a cylinder only through its column ramp: specular just
// left of centre, falling off hard to the right.
const STACK_X = 3;
const STACK_COLS = ["3", "1", "2", "4", "6"]; // left rim -> right rim
const STACK_TOP = 4;
STACK_COLS.forEach((v, i) => rect(STACK_X + i, STACK_TOP, STACK_X + i, HALL_TOP - 1, v));
// the rim flares a pixel each side and catches the light flat-on
rect(STACK_X - 1, STACK_TOP, STACK_X + STACK_COLS.length, STACK_TOP + 1, "2");
rect(STACK_X + 3, STACK_TOP, STACK_X + STACK_COLS.length, STACK_TOP + 1, "5");
// one hoop, the detail that stops the barrel reading as a bare bar
rect(STACK_X - 1, 11, STACK_X + STACK_COLS.length, 11, "5");
rect(STACK_X, 11, STACK_X + 1, 11, "2");

// --- the gear -------------------------------------------------------------
// Rising behind the roofline on the right. Rasterising a real gear by angle
// gives a spiky star at this size, so the teeth are placed by hand on the
// eight compass points — symmetric, two pixels proud of the rim, and legible
// down to a favicon. It overlaps the roof so the silhouette stays one shape.
const GEAR_CX = 24;
const GEAR_CY = 9;
const R_BODY = 4.0;
const inGear = (x, y) => {
	const dx = x - GEAR_CX;
	const dy = y - GEAR_CY;
	if (Math.hypot(dx, dy) <= R_BODY) return true; // the rim
	const ax = Math.abs(dx);
	const ay = Math.abs(dy);
	if (ax <= 1 && ay >= 4 && ay <= 5) return true; // north and south teeth
	if (ay <= 1 && ax >= 4 && ax <= 5) return true; // east and west
	if (ax >= 3 && ax <= 4 && ay >= 3 && ay <= 4) return true; // the four corners
	return false;
};
for (let y = 0; y < S; y++) {
	for (let x = 0; x < S; x++) {
		if (!inGear(x, y)) continue;
		const dx = x - GEAR_CX;
		const dy = y - GEAR_CY;
		if (Math.abs(dx) <= 1 && Math.abs(dy) <= 1) {
			set(x, y, "K"); // punched hub
			continue;
		}
		// the same top-left light as the rest of the mark, in four steps
		const lit = (-dx - dy) / (Math.hypot(dx, dy) || 1);
		// one step darker than the hall so the gear stays silver rather than
		// blowing out to white against a pale page
		set(x, y, lit > 0.9 ? "2" : lit > 0.35 ? "3" : lit > -0.25 ? "4" : lit > -0.75 ? "5" : "6");
	}
}

// --- openings -------------------------------------------------------------
// Glass is the one place the ramp bottoms out to the outline value, so the
// windows stay readable whichever chrome band they land in. Each gets a lit
// sill under it.
const window = (x, y, w = 3, h = 3) => {
	rect(x, y, x + w - 1, y + h - 1, "K");
	rect(x, y + h, x + w - 1, y + h, "2");
};
window(3, 22);
window(8, 22);
window(13, 22);
// the roll-up door, taller, slatted, with a lit lintel
rect(19, 21, 19, 29, "2");
rect(20, 22, 26, 29, "K");
rect(20, 21, 26, 21, "2");
for (let y = 23; y <= 29; y += 2) rect(20, y, 26, y, "6");

// --- outline and floor ----------------------------------------------------
// One near-black pixel wrapping the whole silhouette. It is what lets the
// mark survive being cut out of vinyl and stuck on a dark anodised lid.
const filled = (x, y) => get(x, y) !== ".";
const outline = [];
for (let y = 0; y < S; y++) {
	for (let x = 0; x < S; x++) {
		if (filled(x, y)) continue;
		if (filled(x - 1, y) || filled(x + 1, y) || filled(x, y - 1) || filled(x, y + 1)) {
			outline.push([x, y]);
		}
	}
}
for (const [x, y] of outline) set(x, y, "K");

// --- emit -----------------------------------------------------------------
mkdirSync(OUT, { recursive: true });

const svg = (grid = px) => {
	const n = grid.length;
	const rects = [];
	for (let y = 0; y < n; y++) {
		// merge horizontal runs so the file stays small and the cutter sees
		// clean paths rather than a thousand abutting squares
		let x = 0;
		while (x < n) {
			const v = grid[y][x];
			if (v === ".") {
				x++;
				continue;
			}
			let w = 1;
			while (x + w < n && grid[y][x + w] === v) w++;
			rects.push(`<rect x="${x}" y="${y}" width="${w}" height="1" fill="${PALETTE[v]}"/>`);
			x += w;
		}
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ${n} ${n}" width="${n}" height="${n}" shape-rendering="crispEdges">\n${rects.join("\n")}\n</svg>\n`;
};

const crcTable = Array.from({ length: 256 }, (_, n) => {
	let c = n;
	for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
	return c >>> 0;
});
const crc32 = (buf) => {
	let c = 0xffffffff;
	for (const b of buf) c = crcTable[(c ^ b) & 0xff] ^ (c >>> 8);
	return (c ^ 0xffffffff) >>> 0;
};
const chunk = (type, data) => {
	const len = Buffer.alloc(4);
	len.writeUInt32BE(data.length);
	const body = Buffer.concat([Buffer.from(type, "ascii"), data]);
	const crc = Buffer.alloc(4);
	crc.writeUInt32BE(crc32(body));
	return Buffer.concat([len, body, crc]);
};
const png = (scale, grid = px) => {
	const n = grid.length;
	const size = n * scale;
	const raw = Buffer.alloc(size * (size * 4 + 1));
	let o = 0;
	for (let y = 0; y < size; y++) {
		raw[o++] = 0; // no per-row filter; the art is flats, nothing to predict
		for (let x = 0; x < size; x++) {
			const hex = PALETTE[grid[(y / scale) | 0][(x / scale) | 0]];
			if (!hex) {
				o += 4;
				continue;
			}
			raw[o++] = parseInt(hex.slice(1, 3), 16);
			raw[o++] = parseInt(hex.slice(3, 5), 16);
			raw[o++] = parseInt(hex.slice(5, 7), 16);
			raw[o++] = 255;
		}
	}
	const ihdr = Buffer.alloc(13);
	ihdr.writeUInt32BE(size, 0);
	ihdr.writeUInt32BE(size, 4);
	ihdr[8] = 8; // bit depth
	ihdr[9] = 6; // RGBA
	return Buffer.concat([
		Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
		chunk("IHDR", ihdr),
		chunk("IDAT", deflateSync(raw, { level: 9 })),
		chunk("IEND", Buffer.alloc(0)),
	]);
};

const wrote = [];
const write = (name, data) => {
	writeFileSync(join(OUT, name), data);
	wrote.push(name);
};
write("factory-mark.svg", svg());
write("factory-mark.png", png(1));
write("factory-mark@16x.png", png(16));
write("factory-mark@32x.png", png(32));
write("favicon.png", png(2));

// The sticker cut: two bright pixels of keyline outside the black outline, so
// a die-cut on chrome vinyl has a border to cut against and the mark does not
// bleed into whatever it is stuck to. The keyline needs room the icon grid
// does not have, so it grows on its own padded canvas. Concave pockets — the
// valleys between the roof teeth — fill in, which is what a real die-cut does
// too: a blade cannot turn inside a one-pixel notch.
const PAD = 4;
const SS = S + PAD * 2;
const sticker = Array.from({ length: SS }, () => Array(SS).fill("."));
for (let y = 0; y < S; y++) {
	for (let x = 0; x < S; x++) sticker[y + PAD][x + PAD] = px[y][x];
}
const at = (x, y) => (x >= 0 && x < SS && y >= 0 && y < SS ? sticker[y][x] : ".");
for (let pass = 0; pass < 2; pass++) {
	const ring = [];
	for (let y = 0; y < SS; y++) {
		for (let x = 0; x < SS; x++) {
			if (sticker[y][x] !== ".") continue;
			if (at(x - 1, y) !== "." || at(x + 1, y) !== "." || at(x, y - 1) !== "." || at(x, y + 1) !== ".") {
				ring.push([x, y]);
			}
		}
	}
	for (const [x, y] of ring) sticker[y][x] = "2";
}
write("factory-sticker.svg", svg(sticker));
write("factory-sticker@32x.png", png(32, sticker));
console.log(wrote.map((n) => join("public/brand", n)).join("\n"));
