#!/usr/bin/env python3
"""The AXLR Bots marks, in the Underpass pixel Emblem style (2-pixel strokes
on a 16-row grid, like the KMP, MADE and AXLR emblems).

AXLR keeps its palette, one colour per letter; BOTS takes the same palette
reversed. The A of AXLR is an invader bot and the O of BOTS a one-eye bot.
Writes the one-line emblem, the two-line emblem and the icon, light and dark,
as SVG and PNG (Pillow). Decided with the author on 24 October 2026; no
Spectrum bars."""
import sys

COPPER, GOLD, BROWN, SAND = "#B85C38", "#D97B2D", "#6B4A2E", "#C9B08C"
FONT = {
    "X": ["##      ##", "##      ##", "##      ##", "##      ##", "  ##  ##  ", "  ##  ##  ", "    ##    ", "    ##    ", "  ##  ##  ", "  ##  ##  ", "##      ##", "##      ##", "##      ##", "##      ##"],
    "L": ["##        ", "##        ", "##        ", "##        ", "##        ", "##        ", "##        ", "##        ", "##        ", "##        ", "##        ", "##        ", "##########", "##########"],
    "R": ["########  ", "########  ", "##      ##", "##      ##", "##      ##", "##      ##", "########  ", "########  ", "##  ##    ", "##  ##    ", "##    ##  ", "##    ##  ", "##      ##", "##      ##"],
    "B": ["########  ", "########  ", "##      ##", "##      ##", "##      ##", "##      ##", "########  ", "########  ", "##      ##", "##      ##", "##      ##", "##      ##", "########  ", "########  "],
    "T": ["##########", "##########", "    ##    ", "    ##    ", "    ##    ", "    ##    ", "    ##    ", "    ##    ", "    ##    ", "    ##    ", "    ##    ", "    ##    ", "    ##    ", "    ##    "],
    "S": ["  ########", "  ########", "##        ", "##        ", "##        ", "##        ", "  ######  ", "  ######  ", "        ##", "        ##", "        ##", "        ##", "########  ", "########  "],
}
INVADER = ["      ##      ", "      ##      ", "  ##########  ", "  ##########  ", "  ##  ##  ##  ", "  ##  ##  ##  ", "  ##########  ", "  ##########  ", "##############", "##############", "  ##########  ", "  ##########  ", "  ##      ##  ", "  ##      ##  ", "####      ####", "####      ####"]
ONE_EYE = ["      ##      ", "   ########   ", "  ##########  ", "  ####  ####  ", "  ####  ####  ", "  ##########  ", "   ########   ", "    ######    ", "  ##########  ", "  ##########  ", "  ##########  ", "  ##########  ", "    ##  ##    ", "    ##  ##    ", "  ####  ####  ", "  ####  ####  "]
# (glyph rows, top row on the 16-row grid, colour)
AXLR = [(INVADER, 0, COPPER), (FONT["X"], 1, GOLD), (FONT["L"], 1, BROWN), (FONT["R"], 1, SAND)]
BOTS = [(FONT["B"], 1, SAND), (ONE_EYE, 0, BROWN), (FONT["T"], 1, GOLD), (FONT["S"], 1, COPPER)]
GAP_LETTER, GAP_WORD, GAP_LINE = 2, 6, 2


def word(glyphs, dx=0, dy=0):
    cells, x = [], dx
    for rows, top, colour in glyphs:
        for y, row in enumerate(rows):
            for i, c in enumerate(row):
                if c == "#":
                    cells.append((x + i, dy + top + y, colour))
        x += len(rows[0]) + GAP_LETTER
    return cells, x - GAP_LETTER - dx


def one_line():
    a, wa = word(AXLR)
    b, wb = word(BOTS, dx=wa + GAP_WORD)
    return a + b, wa + GAP_WORD + wb, 16


def two_lines():
    _, wa = word(AXLR)
    _, wb = word(BOTS)
    w = max(wa, wb)
    a, _ = word(AXLR, dx=(w - wa) // 2)
    b, _ = word(BOTS, dx=(w - wb) // 2, dy=16 + GAP_LINE)
    return a + b, w, 16 + GAP_LINE + 16


def icon():
    cells, w = word([(INVADER, 0, COPPER)])
    return cells, w, 16


def svg(cells, w, h, title):
    rects = "".join(f'<rect x="{x}" y="{y}" width="1" height="1" fill="{c}"/>' for x, y, c in cells)
    return (f'<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 {w} {h}" width="{w*8}" height="{h*8}" '
            f'shape-rendering="crispEdges" role="img"><title>{title}</title>{rects}</svg>\n')


def png(cells, w, h, path, scale=8):
    from PIL import Image
    im = Image.new("RGBA", (w * scale, h * scale), (0, 0, 0, 0))
    px = im.load()
    for x, y, c in cells:
        rgb = tuple(int(c[i:i + 2], 16) for i in (1, 3, 5)) + (255,)
        for dy in range(scale):
            for dx in range(scale):
                px[x * scale + dx, y * scale + dy] = rgb
    im.save(path)


here = __file__.rsplit("/", 1)[0] + "/" if "/" in __file__ else ""
for name, (cells, w, h) in {"axlr-bots-emblem": one_line(), "axlr-bots-emblem-stacked": two_lines(), "axlr-bots-icon": icon()}.items():
    open(here + name + ".svg", "w").write(svg(cells, w, h, "AXLR Bots"))
    png(cells, w, h, here + name + ".png")
    print(name, w, "x", h)
