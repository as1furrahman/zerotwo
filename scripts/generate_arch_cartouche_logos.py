import math
import os
from PIL import Image, ImageDraw, ImageFont, ImageOps

HERMES_BLUE = (0, 0, 242)        # #0000F2
HERMES_WHITE = (245, 245, 245)    # #F5F5F5
CRIMSON_RED = (255, 23, 68)       # #FF1744
DARK_OBSIDIAN = (10, 10, 20)      # #0A0A14

SIZE = 2048
CENTER = SIZE // 2

stencil_src = Image.open('assets/zerotwo_face_electric_blue_stencil.png').convert('RGB')

# Helper to render crimson Hanko seal
def render_hanko(size=260, text="零弐"):
    hanko = Image.new('RGBA', (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(hanko)
    pad = int(size * 0.04)
    r = int(size * 0.12)
    draw.rounded_rectangle((pad, pad, size - pad, size - pad), radius=r, fill=(CRIMSON_RED[0], CRIMSON_RED[1], CRIMSON_RED[2], 255))
    in_pad = int(size * 0.09)
    draw.rounded_rectangle((in_pad, in_pad, size - in_pad, size - in_pad), radius=int(r * 0.8), outline=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255), width=max(3, int(size * 0.025)))
    font_sz = int(size * 0.38)
    font = ImageFont.truetype("/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc", font_sz)
    ch1, ch2 = text[0], text[1]
    bb1 = draw.textbbox((0, 0), ch1, font=font)
    bb2 = draw.textbbox((0, 0), ch2, font=font)
    w1, h1 = bb1[2] - bb1[0], bb1[3] - bb1[1]
    w2, h2 = bb2[2] - bb2[0], bb2[3] - bb2[1]
    draw.text(((size - w1)//2, int(size * 0.15)), ch1, font=font, fill=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
    draw.text(((size - w2)//2, int(size * 0.51)), ch2, font=font, fill=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
    return hanko

# =========================================================================
# VARIATION 1: Pure Classical Arch Cartouche (Centered, Double Border)
# The exact cartouche shape from Torii arch, but fully centered, no shrine, no top 02
# =========================================================================
print("Rendering Variation 1: Clean Classical Arch Cartouche...")
# In 2048 canvas, let's center the cartouche:
# Width: 1300, Height: 1640.
# Left: (2048 - 1300)//2 = 374, Right: 374 + 1300 = 1674.
# Top: (2048 - 1640)//2 = 204, Bottom: 204 + 1640 = 1844.
# Radius: 440 px (soft arched dome at top and curved base)

X0, X1 = 360, 1688
Y0, Y1 = 180, 1868
RADIUS = 460

cartouche_1 = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Scaled portrait positioned inside the arch
SCALED_W = int(SIZE * 0.90)
SCALED_H = int(SIZE * 0.90)
stencil_scaled = stencil_src.resize((SCALED_W, SCALED_H), Image.Resampling.LANCZOS)
portrait_layer = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
# Vertically position so horns sit gracefully under the top arch
portrait_layer.paste(stencil_scaled, ((SIZE - SCALED_W) // 2, Y0 + 130))

# Inner clipping mask (inset by 30px)
mask_inner = Image.new('L', (SIZE, SIZE), 0)
d_mi = ImageDraw.Draw(mask_inner)
d_mi.rounded_rectangle((X0 + 26, Y0 + 26, X1 - 26, Y1 - 26), radius=RADIUS - 26, fill=255)
cartouche_1.paste(portrait_layer, mask=mask_inner)

# Draw elegant double white border lines
draw_c1 = ImageDraw.Draw(cartouche_1)
# Outer ring: stroke 22px
draw_c1.rounded_rectangle((X0, Y0, X1, Y1), radius=RADIUS, outline=HERMES_WHITE, width=22)
# Inner ring: stroke 12px (inset by 38px)
draw_c1.rounded_rectangle((X0 + 38, Y0 + 38, X1 - 38, Y1 - 38), radius=RADIUS - 38, outline=HERMES_WHITE, width=12)

# Save 1024 master
c1_1k = cartouche_1.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
c1_1k.save('assets/zerotwo_arch_cartouche_clean.png')

# Save transparent cutout version
c1_trans = cartouche_1.copy()
mask_trans = Image.new('L', (SIZE, SIZE), 0)
d_mt = ImageDraw.Draw(mask_trans)
d_mt.rounded_rectangle((X0 - 4, Y0 - 4, X1 + 4, Y1 + 4), radius=RADIUS + 4, fill=255)
c1_trans.putalpha(mask_trans)
c1_trans_1k = c1_trans.resize((1024, 1024), Image.Resampling.LANCZOS)
c1_trans_1k.save('assets/zerotwo_arch_cartouche_transparent.png')
print("Variation 1 saved.")

# =========================================================================
# VARIATION 2: True Semi-Circular Dome Arch (Architectural Niche)
# Pure 180-degree semi-circle top dome with straight vertical columns and flat/rounded plinth
# =========================================================================
print("Rendering Variation 2: Semi-Circular Dome Arch Cartouche...")
cartouche_2 = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Top dome radius = half width = (1688 - 360)/2 = 664
DOME_R = (X1 - X0) // 2   # 664
BOT_R = 120

def draw_arch_outline(draw_obj, x0, y0, x1, y1, top_r, bot_r, width, color):
    # Top dome arc: 180 to 0 degrees
    draw_obj.arc((x0, y0, x1, y0 + 2*top_r), 180, 0, fill=color, width=width)
    # Straight vertical sides
    draw_obj.line([(x0, y0 + top_r), (x0, y1 - bot_r)], fill=color, width=width)
    draw_obj.line([(x1, y0 + top_r), (x1, y1 - bot_r)], fill=color, width=width)
    # Bottom corners & line
    draw_obj.arc((x0, y1 - 2*bot_r, x0 + 2*bot_r, y1), 90, 180, fill=color, width=width)
    draw_obj.arc((x1 - 2*bot_r, y1 - 2*bot_r, x1, y1), 0, 90, fill=color, width=width)
    draw_obj.line([(x0 + bot_r, y1), (x1 - bot_r, y1)], fill=color, width=width)

def make_dome_arch_mask(size, x0, y0, x1, y1, top_r, bot_r):
    mask = Image.new('L', (size, size), 0)
    d = ImageDraw.Draw(mask)
    d.pieslice((x0, y0, x1, y0 + 2*top_r), 180, 0, fill=255)
    d.rounded_rectangle((x0, y0 + top_r, x1, y1), radius=bot_r, fill=255)
    d.rectangle((x0, y0 + top_r - 2, x1, y0 + top_r + 2), fill=255)
    return mask

# Composite portrait inside dome arch
dome_mask = make_dome_arch_mask(SIZE, X0 + 24, Y0 + 24, X1 - 24, Y1 - 24, DOME_R - 24, BOT_R - 10)
# Shift portrait slightly to align with dome
portrait_dome = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
portrait_dome.paste(stencil_scaled, ((SIZE - SCALED_W) // 2, Y0 + 170))
cartouche_2.paste(portrait_dome, mask=dome_mask)

draw_c2 = ImageDraw.Draw(cartouche_2)
# Outer dome arch: width 22
draw_arch_outline(draw_c2, X0, Y0, X1, Y1, DOME_R, BOT_R, width=22, color=HERMES_WHITE)
# Inner dome arch: width 12, inset by 36px
draw_arch_outline(draw_c2, X0 + 36, Y0 + 36, X1 - 36, Y1 - 36, DOME_R - 36, BOT_R - 18, width=12, color=HERMES_WHITE)

c2_1k = cartouche_2.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
c2_1k.save('assets/zerotwo_arch_cartouche_dome.png')
print("Variation 2 saved.")

# =========================================================================
# VARIATION 3: Arch Cartouche with Traditional Lower Hanko Stamp
# Same clean arch cartouche, with an authentic crimson Hanko seal (「零弐」) in lower-right
# =========================================================================
print("Rendering Variation 3: Arch Cartouche with Crimson Hanko Stamp...")
cartouche_3 = cartouche_1.copy()
hanko_stamp = render_hanko(size=250, text="零弐")
cartouche_3.paste(hanko_stamp, (X1 - 330, Y1 - 330), hanko_stamp)

c3_1k = cartouche_3.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
c3_1k.save('assets/zerotwo_arch_cartouche_hanko.png')
print("Variation 3 saved.")

# =========================================================================
# VARIATION 4: Modernist Stepped-Base Arch Cartouche (Architectural Stela)
# Features a traditional stepped base pedestal at the bottom for grounded weight
# =========================================================================
print("Rendering Variation 4: Stepped Base Architectural Arch Cartouche...")
cartouche_4 = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))
cartouche_4.paste(portrait_layer, mask=mask_inner)

draw_c4 = ImageDraw.Draw(cartouche_4)
# Arch dome top & straight sides down to stepped base
draw_c4.rounded_rectangle((X0, Y0, X1, Y1 - 80), radius=RADIUS, outline=HERMES_WHITE, width=22)
draw_c4.rounded_rectangle((X0 + 38, Y0 + 38, X1 - 38, Y1 - 118), radius=RADIUS - 38, outline=HERMES_WHITE, width=12)

# Stepped plinth at base
draw_c4.rectangle((X0 - 60, Y1 - 80, X1 + 60, Y1 - 20), fill=HERMES_WHITE)
draw_c4.rectangle((X0 - 100, Y1 - 20, X1 + 100, Y1 + 30), fill=HERMES_WHITE)
# Inset blue detail line in plinth
draw_c4.line([(X0 - 40, Y1 - 50), (X1 + 40, Y1 - 50)], fill=HERMES_BLUE, width=6)

c4_1k = cartouche_4.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
c4_1k.save('assets/zerotwo_arch_cartouche_stepped_base.png')
print("Variation 4 saved.")

print("All Arch Cartouche designs generated successfully!")
