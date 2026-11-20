"""Draws the round "APPROVED" seal (seal.png) used by build.sh. Needs Pillow.

The seal is a plain transparent PNG; pidief only places it on the page (pidief stamp <pdf> seal.png).
Usage: python make-seal.py [fonts-dir] [output]
"""
import math
import random
import sys

from PIL import Image, ImageDraw, ImageFont

FONTS = sys.argv[1] if len(sys.argv) > 1 else "../../internal/assets/fonts"
OUT = sys.argv[2] if len(sys.argv) > 2 else "seal.png"
RING = "EXAMPLE CLOUD INC.  •  FINANCE  •  "
MIDDLE, DATE = "APPROVED", "NOV 20, 2026"

S, INK = 900, (205, 52, 34, 255)
img = Image.new("RGBA", (S, S), (0, 0, 0, 0))
draw = ImageDraw.Draw(img)
c = S / 2
draw.ellipse((20, 20, S - 20, S - 20), outline=INK, width=16)
draw.ellipse((70, 70, S - 70, S - 70), outline=INK, width=5)
draw.ellipse((235, 235, S - 235, S - 235), outline=INK, width=5)

# ring text: one glyph at a time, rotated along a circle
bold = ImageFont.truetype(FONTS + "/Onest-Bold.ttf", 60)
r = 300
scale = 2 * math.pi * r / sum(bold.getlength(ch) for ch in RING)
angle = -90.0
for ch in RING:
    w = bold.getlength(ch) * scale
    a = angle + math.degrees(w / 2 / r)
    glyph = Image.new("RGBA", (120, 120), (0, 0, 0, 0))
    ImageDraw.Draw(glyph).text((60, 60), ch, font=bold, fill=INK, anchor="mm")
    glyph = glyph.rotate(-(a + 90), resample=Image.BICUBIC)
    img.alpha_composite(glyph, (int(c + r * math.cos(math.radians(a)) - 60), int(c + r * math.sin(math.radians(a)) - 60)))
    angle += math.degrees(w / r)

draw.text((c, c - 40), MIDDLE, font=ImageFont.truetype(FONTS + "/Onest-Bold.ttf", 76), fill=INK, anchor="mm")
draw.line((c - 150, c + 12, c + 150, c + 12), fill=INK, width=5)
draw.text((c, c + 62), DATE, font=ImageFont.truetype(FONTS + "/Onest-SemiBold.ttf", 42), fill=INK, anchor="mm")

# uneven ink, like a rubber stamp
random.seed(3)
px = img.load()
for _ in range(9000):
    x, y = random.randrange(S), random.randrange(S)
    if px[x, y][3]:
        px[x, y] = (INK[0], INK[1], INK[2], random.randint(40, 170))
img.rotate(-14, resample=Image.BICUBIC).resize((S // 2, S // 2), Image.LANCZOS).save(OUT, optimize=True)
print("wrote", OUT)
