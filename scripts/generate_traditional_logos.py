import math
import os
from PIL import Image, ImageDraw, ImageFont, ImageOps

HERMES_BLUE = (0, 0, 242)        # #0000F2
HERMES_WHITE = (245, 245, 245)    # #F5F5F5
CRIMSON_RED = (255, 23, 68)       # #FF1744
DARK_OBSIDIAN = (10, 10, 20)      # #0A0A14

SIZE = 2048
CENTER = SIZE // 2

# Fonts
cjk_font_large = ImageFont.truetype("/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc", 110)
cjk_font_med = ImageFont.truetype("/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc", 80)
sans_font_bold = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 90)
serif_font_arc = ImageFont.truetype("/usr/share/fonts/truetype/liberation/LiberationSerif-Bold.ttf", 64)
serif_font_arc_sm = ImageFont.truetype("/usr/share/fonts/truetype/liberation/LiberationSerif-Bold.ttf", 52)

# Load raw stencil
stencil_src = Image.open('assets/zerotwo_face_electric_blue_stencil.png').convert('RGB')

# -------------------------------------------------------------------------
# Helper Functions
# -------------------------------------------------------------------------
def render_hanko(size=260, text="零弐", is_kanji=True):
    hanko = Image.new('RGBA', (size, size), (0, 0, 0, 0))
    draw = ImageDraw.Draw(hanko)
    pad = int(size * 0.04)
    r = int(size * 0.12)
    # Red rounded box
    draw.rounded_rectangle((pad, pad, size - pad, size - pad), radius=r, fill=(CRIMSON_RED[0], CRIMSON_RED[1], CRIMSON_RED[2], 255))
    # Inset thin white border
    in_pad = int(size * 0.09)
    draw.rounded_rectangle((in_pad, in_pad, size - in_pad, size - in_pad), radius=int(r * 0.8), outline=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255), width=max(3, int(size * 0.025)))
    
    if is_kanji and len(text) == 2:
        font_sz = int(size * 0.38)
        font = ImageFont.truetype("/usr/share/fonts/opentype/noto/NotoSerifCJK-Bold.ttc", font_sz)
        ch1, ch2 = text[0], text[1]
        bb1 = draw.textbbox((0, 0), ch1, font=font)
        bb2 = draw.textbbox((0, 0), ch2, font=font)
        w1, h1 = bb1[2] - bb1[0], bb1[3] - bb1[1]
        w2, h2 = bb2[2] - bb2[0], bb2[3] - bb2[1]
        draw.text(((size - w1)//2, int(size * 0.15)), ch1, font=font, fill=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
        draw.text(((size - w2)//2, int(size * 0.51)), ch2, font=font, fill=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
    else:
        font_sz = int(size * 0.44)
        font = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", font_sz)
        bb = draw.textbbox((0, 0), text, font=font)
        w, h = bb[2] - bb[0], bb[3] - bb[1]
        draw.text(((size - w)//2, (size - h)//2 - int(size*0.04)), text, font=font, fill=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
    return hanko

def render_curved_text(base_img, text, cx, cy, radius, start_angle, end_angle, font, fill_color, inward=True):
    n = len(text)
    if n == 0:
        return
    angles = [start_angle + (end_angle - start_angle) * (i / max(1, n - 1)) for i in range(n)]
    for ch, ang in zip(text, angles):
        rad = math.radians(ang)
        x = cx + radius * math.cos(rad)
        y = cy + radius * math.sin(rad)
        patch_size = 280
        patch = Image.new('RGBA', (patch_size, patch_size), (0, 0, 0, 0))
        pdraw = ImageDraw.Draw(patch)
        bbox = pdraw.textbbox((0, 0), ch, font=font)
        ch_w = bbox[2] - bbox[0]
        ch_h = bbox[3] - bbox[1]
        pdraw.text(((patch_size - ch_w)//2, (patch_size - ch_h)//2), ch, font=font, fill=fill_color)
        rot_deg = -(ang + 90) if inward else -(ang - 90)
        rotated_patch = patch.rotate(rot_deg, resample=Image.Resampling.BICUBIC)
        px = int(x - patch_size // 2)
        py = int(y - patch_size // 2)
        base_img.paste(rotated_patch, (px, py), rotated_patch)

def get_octagon_points(cx, cy, radius):
    pts = []
    for i in range(8):
        ang = math.radians(22.5 + i * 45)
        pts.append((cx + radius * math.cos(ang), cy + radius * math.sin(ang)))
    return pts

# Prepare scaled centered stencil portrait
# Scale factor: 0.86 to ensure entire horns & face sit comfortably inside radius 830
SCALED_W = int(SIZE * 0.86)
SCALED_H = int(SIZE * 0.86)
stencil_scaled = stencil_src.resize((SCALED_W, SCALED_H), Image.Resampling.LANCZOS)
OFFSET_X = (SIZE - SCALED_W) // 2
OFFSET_Y = (SIZE - SCALED_H) // 2 + 50

# Base portrait layer
portrait_layer = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
portrait_layer.paste(stencil_scaled, (OFFSET_X, OFFSET_Y))

# -------------------------------------------------------------------------
# VARIATION 1: Traditional Japanese Kamon (Crest Roundel) with Cardinal Notches
# -------------------------------------------------------------------------
print("Generating Variation 1: Traditional Kamon Roundel...")
kamon = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Circular inner field clipping
clip_mask = Image.new('L', (SIZE, SIZE), 0)
draw_clip = ImageDraw.Draw(clip_mask)
draw_clip.ellipse((CENTER - 830, CENTER - 830, CENTER + 830, CENTER + 830), fill=255)
kamon.paste(portrait_layer, mask=clip_mask)

# Draw Kamon Concentric Rings
draw_kamon = ImageDraw.Draw(kamon)
# Outer ring: R=885, width=22
draw_kamon.ellipse((CENTER - 885, CENTER - 885, CENTER + 885, CENTER + 885), outline=HERMES_WHITE, width=22)
# Inner ring: R=836, width=12
draw_kamon.ellipse((CENTER - 836, CENTER - 836, CENTER + 836, CENTER + 836), outline=HERMES_WHITE, width=12)

# Traditional Cardinal Notch Accents on outer ring (North, South, East, West)
# Cut small gaps and place decorative diamond studs
for angle in [0, 90, 180, 270]:
    rad = math.radians(angle)
    # Center of diamond on outer ring
    dx = CENTER + 885 * math.cos(rad)
    dy = CENTER + 885 * math.sin(rad)
    d_size = 28
    diamond = [
        (dx, dy - d_size),
        (dx + d_size, dy),
        (dx, dy + d_size),
        (dx - d_size, dy)
    ]
    # Blue cutout backing
    cut_size = 40
    cutout = [
        (dx - cut_size*math.cos(rad) - cut_size*math.sin(rad), dy - cut_size*math.sin(rad) + cut_size*math.cos(rad)),
        (dx + cut_size*math.cos(rad) - cut_size*math.sin(rad), dy + cut_size*math.sin(rad) + cut_size*math.cos(rad)),
        (dx + cut_size*math.cos(rad) + cut_size*math.sin(rad), dy + cut_size*math.sin(rad) - cut_size*math.cos(rad)),
        (dx - cut_size*math.cos(rad) + cut_size*math.sin(rad), dy - cut_size*math.sin(rad) - cut_size*math.cos(rad))
    ]
    # Draw decorative diamond stud
    draw_kamon.polygon(diamond, fill=HERMES_WHITE)
    # Center blue dot in diamond
    draw_kamon.ellipse((dx - 6, dy - 6, dx + 6, dy + 6), fill=HERMES_BLUE)

# Place Traditional Hanko Seal in lower right
hanko_rei_ni = render_hanko(size=280, text="零弐", is_kanji=True)
kamon.paste(hanko_rei_ni, (1320, 1320), hanko_rei_ni)

# Save 1024x1024 master
kamon_1k = kamon.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
kamon_1k.save('assets/zerotwo_traditional_kamon_blue.png')

# Also save transparent version
kamon_trans = kamon.copy()
# Create circular alpha mask for outer edge
trans_mask = Image.new('L', (SIZE, SIZE), 0)
draw_trans = ImageDraw.Draw(trans_mask)
draw_trans.ellipse((CENTER - 898, CENTER - 898, CENTER + 898, CENTER + 898), fill=255)
# Also keep diamond studs in mask
for angle in [0, 90, 180, 270]:
    rad = math.radians(angle)
    dx = CENTER + 885 * math.cos(rad)
    dy = CENTER + 885 * math.sin(rad)
    draw_trans.ellipse((dx - 32, dy - 32, dx + 32, dy + 32), fill=255)
kamon_trans.putalpha(trans_mask)
kamon_trans_1k = kamon_trans.resize((1024, 1024), Image.Resampling.LANCZOS)
kamon_trans_1k.save('assets/zerotwo_traditional_kamon_transparent.png')
print("Variation 1 saved.")

# -------------------------------------------------------------------------
# VARIATION 2: Neoclassical Hermes Medallion / Coin Emblem
# -------------------------------------------------------------------------
print("Generating Variation 2: Neoclassical Hermes Medallion...")
medal = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Inner core circle (radius 680)
clip_medal = Image.new('L', (SIZE, SIZE), 0)
draw_cmedal = ImageDraw.Draw(clip_medal)
draw_cmedal.ellipse((CENTER - 680, CENTER - 680, CENTER + 680, CENTER + 680), fill=255)

# Slightly scaled portrait to fit inside core radius 680
SCALE_MED = 0.72
MED_W = int(SIZE * SCALE_MED)
MED_H = int(SIZE * SCALE_MED)
stencil_med = stencil_src.resize((MED_W, MED_H), Image.Resampling.LANCZOS)
med_portrait = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
med_portrait.paste(stencil_med, ((SIZE - MED_W)//2, (SIZE - MED_H)//2 + 40))
medal.paste(med_portrait, mask=clip_medal)

draw_medal = ImageDraw.Draw(medal)

# 1. Beaded perimeter rim (68 beads at R=895)
num_beads = 68
bead_r = 895
for i in range(num_beads):
    ang = 2 * math.pi * i / num_beads
    bx = CENTER + bead_r * math.cos(ang)
    by = CENTER + bead_r * math.sin(ang)
    draw_medal.ellipse((bx - 10, by - 10, bx + 10, by + 10), fill=HERMES_WHITE)

# 2. Concentric stepped coin grooves
draw_medal.ellipse((CENTER - 865, CENTER - 865, CENTER + 865, CENTER + 865), outline=HERMES_WHITE, width=10)
draw_medal.ellipse((CENTER - 845, CENTER - 845, CENTER + 845, CENTER + 845), outline=HERMES_WHITE, width=5)
draw_medal.ellipse((CENTER - 690, CENTER - 690, CENTER + 690, CENTER + 690), outline=HERMES_WHITE, width=12)

# 3. Curved text inscriptions
# Top arc: "ZEROTWO • CODE 002"
render_curved_text(medal, "ZEROTWO • CODE 002", CENTER, CENTER, 765, -145, -35, serif_font_arc, (HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255), inward=True)
# Bottom arc: "★ DARLING IN THE FRANXX ★"
render_curved_text(medal, "★ DARLING IN THE FRANXX ★", CENTER, CENTER, 765, 145, 35, serif_font_arc_sm, (HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255), inward=False)

# Save Variation 2
medal_1k = medal.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
medal_1k.save('assets/zerotwo_traditional_medallion_blue.png')
print("Variation 2 saved.")

# -------------------------------------------------------------------------
# VARIATION 3: Traditional Hakkaku-mon (Imperial Mecha Octagonal Seal)
# -------------------------------------------------------------------------
print("Generating Variation 3: Traditional Octagonal Imperial Seal...")
oct_img = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Octagonal mask for inner portrait (radius 830)
oct_clip = Image.new('L', (SIZE, SIZE), 0)
draw_oc = ImageDraw.Draw(oct_clip)
oct_pts_inner = get_octagon_points(CENTER, CENTER, 830)
draw_oc.polygon(oct_pts_inner, fill=255)

oct_img.paste(portrait_layer, mask=oct_clip)

draw_oct = ImageDraw.Draw(oct_img)
# Outer Octagon: R=885, stroke=22
oct_pts_outer = get_octagon_points(CENTER, CENTER, 885)
for i in range(8):
    p1 = oct_pts_outer[i]
    p2 = oct_pts_outer[(i + 1) % 8]
    draw_oct.line([p1, p2], fill=HERMES_WHITE, width=22)

# Inner Octagon: R=840, stroke=10
for i in range(8):
    p1 = oct_pts_inner[i]
    p2 = oct_pts_inner[(i + 1) % 8]
    draw_oct.line([p1, p2], fill=HERMES_WHITE, width=10)

# Corner ornamental brackets at each of the 8 vertices
for p in oct_pts_outer:
    draw_oct.ellipse((p[0] - 14, p[1] - 14, p[0] + 14, p[1] + 14), fill=HERMES_WHITE)

# Traditional Japanese cardinal tabs at 4 cardinal flat edges (Top, Bottom, Left, Right)
# In an octagon with 22.5 deg offset, the edges at 0, 90, 180, 270 deg are orthogonal
# Add Hanko seal at bottom center
hanko_oct = render_hanko(size=240, text="02", is_kanji=False)
oct_img.paste(hanko_oct, (CENTER - 120, CENTER + 570), hanko_oct)

# Save Variation 3
oct_1k = oct_img.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
oct_1k.save('assets/zerotwo_traditional_octagonal_blue.png')
print("Variation 3 saved.")

# -------------------------------------------------------------------------
# VARIATION 4: Minimalist Kamon Emblem (Maximum Avatar / Favicon Impact)
# -------------------------------------------------------------------------
print("Generating Variation 4: Minimalist Kamon Emblem...")
mini = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Inner circular field
clip_mini = Image.new('L', (SIZE, SIZE), 0)
draw_cmini = ImageDraw.Draw(clip_mini)
draw_cmini.ellipse((CENTER - 830, CENTER - 830, CENTER + 830, CENTER + 830), fill=255)
mini.paste(portrait_layer, mask=clip_mini)

draw_mini = ImageDraw.Draw(mini)
# Crisp double rings
draw_mini.ellipse((CENTER - 880, CENTER - 880, CENTER + 880, CENTER + 880), outline=HERMES_WHITE, width=24)
draw_mini.ellipse((CENTER - 836, CENTER - 836, CENTER + 836, CENTER + 836), outline=HERMES_WHITE, width=12)

# Hanko seal with stylized bold "02"
hanko_mini = render_hanko(size=260, text="02", is_kanji=False)
mini.paste(hanko_mini, (1330, 1330), hanko_mini)

# Save Variation 4
mini_1k = mini.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
mini_1k.save('assets/zerotwo_traditional_kamon_minimal.png')
print("Variation 4 saved.")

print("All variations generated successfully!")
