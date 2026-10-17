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

# Exact scaled portrait from the original Torii design:
# In the original Torii script:
# SCALED_W = int(SIZE * 0.86) = 1761
# SCALED_H = int(SIZE * 0.86) = 1761
# portrait_layer.paste(stencil_scaled, ((SIZE - SCALED_W) // 2, (SIZE - SCALED_H) // 2 + 50))
# Let's inspect: (2048 - 1761)//2 = 143; 143 + 50 = 193.
SCALED_W = int(SIZE * 0.86)
SCALED_H = int(SIZE * 0.86)
stencil_scaled = stencil_src.resize((SCALED_W, SCALED_H), Image.Resampling.LANCZOS)

# In original Torii design, the arch was at:
# x0 = 380, x1 = 1668 (width = 1288)
# y0 = 420, y1 = 1780 (height = 1360)
# radius = 380, width = 16
# Notice: vertically, y went from 420 to 1780 because shrine was above y=420.
# If we center this exact cartouche vertically:
# Total height = 1360.
# Centered y0 = (2048 - 1360)//2 = 344. y1 = 344 + 1360 = 1704.
# And the portrait inside moves up by: 420 - 344 = 76px (so portrait y offset is 193 - 76 = 117).
# This perfectly preserves the exact face-to-arch proportion of the original Torii arch!

# -------------------------------------------------------------------------
# VARIATION A: The Exact Centered Torii Arch Cartouche (Clean Single Border)
# -------------------------------------------------------------------------
print("Rendering Option A: Exact Centered Arch Cartouche (Single Border)...")
img_a = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

x0, x1 = 380, 1668
y0, y1 = 344, 1704
rad = 380

# Portrait layer shifted to match centered arch
portrait_a = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
portrait_a.paste(stencil_scaled, ((SIZE - SCALED_W) // 2, 117))

# Arch clip mask
clip_a = Image.new('L', (SIZE, SIZE), 0)
d_ca = ImageDraw.Draw(clip_a)
d_ca.rounded_rectangle((x0, y0, x1, y1), radius=rad, fill=255)
img_a.paste(portrait_a, mask=clip_a)

# Draw exact single white arch border (width = 18px)
draw_a = ImageDraw.Draw(img_a)
draw_a.rounded_rectangle((x0, y0, x1, y1), radius=rad, outline=HERMES_WHITE, width=18)

out_a = img_a.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
out_a.save('assets/zerotwo_arch_cartouche_exact_single.png')
print("Option A saved to assets/zerotwo_arch_cartouche_exact_single.png")

# Also save transparent version of Option A
img_a_trans = img_a.copy()
mask_trans_a = Image.new('L', (SIZE, SIZE), 0)
d_mta = ImageDraw.Draw(mask_trans_a)
d_mta.rounded_rectangle((x0 - 4, y0 - 4, x1 + 4, y1 + 4), radius=rad + 4, fill=255)
img_a_trans.putalpha(mask_trans_a)
out_a_trans = img_a_trans.resize((1024, 1024), Image.Resampling.LANCZOS)
out_a_trans.save('assets/zerotwo_arch_cartouche_exact_transparent.png')

# -------------------------------------------------------------------------
# VARIATION B: Centered Arch Cartouche with Double Concentric Border
# Outer ring + negative space gap + inner precision ring (classic emblem style)
# -------------------------------------------------------------------------
print("Rendering Option B: Centered Arch Cartouche (Double Border)...")
img_b = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Outer border: x0, y0, x1, y1
# Inner border: inset by 32px
gap = 32
inner_rad = rad - gap
clip_b = Image.new('L', (SIZE, SIZE), 0)
d_cb = ImageDraw.Draw(clip_b)
d_cb.rounded_rectangle((x0 + gap, y0 + gap, x1 - gap, y1 - gap), radius=inner_rad, fill=255)
img_b.paste(portrait_a, mask=clip_b)

draw_b = ImageDraw.Draw(img_b)
# Outer line
draw_b.rounded_rectangle((x0, y0, x1, y1), radius=rad, outline=HERMES_WHITE, width=20)
# Inner line
draw_b.rounded_rectangle((x0 + gap, y0 + gap, x1 - gap, y1 - gap), radius=inner_rad, outline=HERMES_WHITE, width=10)

out_b = img_b.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
out_b.save('assets/zerotwo_arch_cartouche_exact_double.png')
print("Option B saved to assets/zerotwo_arch_cartouche_exact_double.png")

# -------------------------------------------------------------------------
# VARIATION C: Full Semi-Circular Arch Dome (Complete 180° Dome Top)
# -------------------------------------------------------------------------
print("Rendering Option C: Full Semi-Circular Arch Dome...")
img_c = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# In 2048, width = 1288. Half-width = 644.
# Full dome: top curve has radius = 644.
dome_r = 644
bot_r = 80
# Center vertically:
# Height = 1460 (top dome from y=294 to 294+644=938; vertical sides from 938 to 1754).
cy0 = 280
cy1 = 1760

def draw_true_arch(draw_obj, px0, py0, px1, py1, tr, br, stroke_w, color):
    draw_obj.arc((px0, py0, px1, py0 + 2*tr), 180, 0, fill=color, width=stroke_w)
    draw_obj.line([(px0, py0 + tr), (px0, py1 - br)], fill=color, width=stroke_w)
    draw_obj.line([(px1, py0 + tr), (px1, py1 - br)], fill=color, width=stroke_w)
    draw_obj.arc((px0, py1 - 2*br, px0 + 2*br, py1), 90, 180, fill=color, width=stroke_w)
    draw_obj.arc((px1 - 2*br, py1 - 2*br, px1, py1), 0, 90, fill=color, width=stroke_w)
    draw_obj.line([(px0 + br, py1), (px1 - br, py1)], fill=color, width=stroke_w)

def make_true_arch_mask(size, px0, py0, px1, py1, tr, br):
    m = Image.new('L', (size, size), 0)
    d = ImageDraw.Draw(m)
    d.pieslice((px0, py0, px1, py0 + 2*tr), 180, 0, fill=255)
    d.rounded_rectangle((px0, py0 + tr, px1, py1), radius=br, fill=255)
    d.rectangle((px0, py0 + tr - 2, px1, py0 + tr + 2), fill=255)
    return m

clip_c = make_true_arch_mask(SIZE, x0 + 16, cy0 + 16, x1 - 16, cy1 - 16, dome_r - 16, bot_r - 8)
# Shift portrait so horns sit right under the semi-circle dome
portrait_c = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
portrait_c.paste(stencil_scaled, ((SIZE - SCALED_W) // 2, 100))
img_c.paste(portrait_c, mask=clip_c)

draw_c = ImageDraw.Draw(img_c)
draw_true_arch(draw_c, x0, cy0, x1, cy1, dome_r, bot_r, stroke_w=20, color=HERMES_WHITE)
draw_true_arch(draw_c, x0 + 28, cy0 + 28, x1 - 28, cy1 - 28, dome_r - 28, bot_r - 14, stroke_w=10, color=HERMES_WHITE)

out_c = img_c.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
out_c.save('assets/zerotwo_arch_cartouche_full_dome.png')
print("Option C saved to assets/zerotwo_arch_cartouche_full_dome.png")

# -------------------------------------------------------------------------
# VARIATION D: Arch Cartouche with Traditional Crimson Hanko Stamp
# -------------------------------------------------------------------------
print("Rendering Option D: Arch Cartouche with Traditional Hanko Seal...")
img_d = img_a.copy()
def render_hanko(size=250, text="零弐"):
    h = Image.new('RGBA', (size, size), (0, 0, 0, 0))
    d = ImageDraw.Draw(h)
    pad = int(size * 0.04)
    r = int(size * 0.12)
    d.rounded_rectangle((pad, pad, size - pad, size - pad), radius=r, fill=(CRIMSON_RED[0], CRIMSON_RED[1], CRIMSON_RED[2], 255))
    in_pad = int(size * 0.09)
    d.rounded_rectangle((in_pad, in_pad, size - in_pad, size - in_pad), radius=int(r * 0.8), outline=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255), width=max(3, int(size * 0.025)))
    font_sz = int(size * 0.38)
    font = ImageFont.truetype("/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc", font_sz)
    ch1, ch2 = text[0], text[1]
    bb1 = d.textbbox((0, 0), ch1, font=font)
    bb2 = d.textbbox((0, 0), ch2, font=font)
    w1, h1 = bb1[2] - bb1[0], bb1[3] - bb1[1]
    w2, h2 = bb2[2] - bb2[0], bb2[3] - bb2[1]
    d.text(((size - w1)//2, int(size * 0.15)), ch1, font=font, fill=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
    d.text(((size - w2)//2, int(size * 0.51)), ch2, font=font, fill=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
    return h

hanko = render_hanko(size=250, text="零弐")
# Place in lower right inside arch
img_d.paste(hanko, (x1 - 320, y1 - 320), hanko)
out_d = img_d.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
out_d.save('assets/zerotwo_arch_cartouche_exact_hanko.png')
print("Option D saved to assets/zerotwo_arch_cartouche_exact_hanko.png")

print("All exact arch cartouche designs rendered successfully!")
