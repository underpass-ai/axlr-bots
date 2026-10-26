"""Containers drawn into an aerial photo where the simulation says they float.

The imagery is real (PDOK's aerial photo, or Esri's out at sea, as the vision module fetches it)
and has no containers in the sea, so the ground station draws them in
before the vision model looks: a top view of a 20 or 40 ft box at its
true size for the photo's scale, turned to its heading, its corrugated
roof in its colour, low in the water with a ring of foam and a little
wake. The model is then asked what it sees; nothing tells it where.
"""

import io
import math
import random

from PIL import Image, ImageDraw, ImageFilter

SCALE = 4                # draw 4× larger and shrink: smooth edges
WIDTH_M = 2.44           # an ISO container's width


def mercator(lat, lon):
    """Web Mercator metres (EPSG:3857) of a point."""
    r = 6378137.0
    x = math.radians(lon) * r
    y = math.log(math.tan(math.pi / 4 + math.radians(lat) / 2)) * r
    return x, y


def _hex(colour):
    colour = colour.lstrip("#")
    return tuple(int(colour[i:i + 2], 16) for i in (0, 2, 4))


def _shade(rgb, k):
    return tuple(max(0, min(255, int(c * k))) for c in rgb)


def _sprite(length_px, width_px, colour, heading_deg, seed):
    """One container in RGBA, centred, already turned."""
    rng = random.Random(seed)
    L, W = max(4, int(length_px * SCALE)), max(2, int(width_px * SCALE))
    pad = int(max(L, W) * 0.45) + 8
    size = L + 2 * pad
    img = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)
    x0, y0 = pad, (size - W) // 2
    # Foam and wake: a soft white rim, longer downstream.
    foam = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    fd = ImageDraw.Draw(foam)
    rim = max(2, W // 3)
    fd.rounded_rectangle([x0 - rim, y0 - rim, x0 + L + rim * 3, y0 + W + rim], radius=rim, fill=(235, 240, 240, 150))
    foam = foam.filter(ImageFilter.GaussianBlur(rim * 0.8))
    img.alpha_composite(foam)
    base = _hex(colour)
    d.rectangle([x0, y0, x0 + L, y0 + W], fill=base + (255,))
    # The corrugation: thin ribs across the roof, lighter and darker.
    rib = max(2, L // 40)
    for i, x in enumerate(range(x0, x0 + L, rib * 2)):
        d.rectangle([x, y0, min(x + rib, x0 + L), y0 + W], fill=_shade(base, 1.18 if i % 2 else 0.85) + (255,))
    # Door end and corner castings: darker bands at both ends.
    end = max(2, L // 24)
    d.rectangle([x0, y0, x0 + end, y0 + W], fill=_shade(base, 0.6) + (255,))
    d.rectangle([x0 + L - end, y0, x0 + L, y0 + W], fill=_shade(base, 0.6) + (255,))
    # Low in the water: one long side under a film of sea.
    sea = Image.new("RGBA", (size, size), (0, 0, 0, 0))
    ImageDraw.Draw(sea).rectangle([x0, y0 + int(W * rng.uniform(0.55, 0.75)), x0 + L, y0 + W], fill=(20, 60, 60, 110))
    img.alpha_composite(sea)
    img = img.rotate(-heading_deg + 90, resample=Image.BICUBIC, expand=False)
    return img


def compose(jpeg, bbox_merc, floaters, quality=88):
    """The photo with the floaters drawn in, and where each one landed.

    bbox_merc is (x1, y1, x2, y2) in Web Mercator metres, the photo's
    extent; each floater is (ident, lat, lon, heading_deg, length_m,
    colour). Returns (jpeg bytes, [(ident, x 0..1, y 0..1)]) for those
    inside the frame.
    """
    photo = Image.open(io.BytesIO(jpeg)).convert("RGBA")
    w, h = photo.size
    x1, y1, x2, y2 = bbox_merc
    m_per_px = (x2 - x1) / w
    placed = []
    for ident, lat, lon, heading, length_m, colour in floaters:
        mx, my = mercator(lat, lon)
        px, py = (mx - x1) / (x2 - x1) * w, (y2 - my) / (y2 - y1) * h
        if not (-20 <= px <= w + 20 and -20 <= py <= h + 20):
            continue
        # Mercator metres are ground metres / cos(lat): scale the box the same way.
        stretch = 1 / math.cos(math.radians(lat))
        sprite = _sprite(length_m * stretch / m_per_px, WIDTH_M * stretch / m_per_px, colour, heading, ident)
        small = sprite.resize((max(1, sprite.width // SCALE), max(1, sprite.height // SCALE)), Image.LANCZOS)
        photo.alpha_composite(small, (int(px - small.width / 2), int(py - small.height / 2)))
        if 0 <= px <= w and 0 <= py <= h:
            placed.append((ident, px / w, py / h))
    out = io.BytesIO()
    photo.convert("RGB").save(out, "JPEG", quality=quality)
    return out.getvalue(), placed
