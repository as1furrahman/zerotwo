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
sans_font_bold = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 80)
serif_font_arc = ImageFont.truetype("/usr/share/fonts/truetype/liberation/LiberationSerif-Bold.ttf", 60)

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
    draw.rounded_rectangle((pad, pad, size - pad, size - pad), radius=r, fill=(CRIMSON_RED[0], CRIMSON_RED[1], CRIMSON_RED[2], 255))
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

# Scaled centered portrait layers
# Standard centered (for closed shapes)
SCALED_W = int(SIZE * 0.86)
SCALED_H = int(SIZE * 0.86)
stencil_scaled = stencil_src.resize((SCALED_W, SCALED_H), Image.Resampling.LANCZOS)
portrait_layer = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
portrait_layer.paste(stencil_scaled, ((SIZE - SCALED_W) // 2, (SIZE - SCALED_H) // 2 + 50))

# Slightly larger portrait positioned specifically so horns break through top
# Scale 0.95, centered vertically around y=1024
SCALED_W_LARGE = int(SIZE * 0.96)
SCALED_H_LARGE = int(SIZE * 0.96)
stencil_large = stencil_src.resize((SCALED_W_LARGE, SCALED_H_LARGE), Image.Resampling.LANCZOS)
portrait_large = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
portrait_large.paste(stencil_large, ((SIZE - SCALED_W_LARGE) // 2, (SIZE - SCALED_H_LARGE) // 2 + 40))

# -------------------------------------------------------------------------
# DESIGN 5: The "Horns Crown / Frame-Breaker" Kamon Crest
# -------------------------------------------------------------------------
print("Generating Design 5: Horns Frame-Breaker Crest...")
fb = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Draw rings with an intentional opening at the top arc (-130 to -50 deg)
# where the crimson horns pierce through
R_out = 840
R_in = 790
mask_rings = Image.new('L', (SIZE, SIZE), 0)
d_rings = ImageDraw.Draw(mask_rings)
d_rings.ellipse((CENTER - R_out, CENTER - R_out, CENTER + R_out, CENTER + R_out), outline=255, width=22)
d_rings.ellipse((CENTER - R_in, CENTER - R_in, CENTER + R_in, CENTER + R_in), outline=255, width=12)

# Mask out top sector (between -135 and -45 deg)
mask_cut = Image.new('L', (SIZE, SIZE), 255)
d_cut = ImageDraw.Draw(mask_cut)
# Wedge from center between -135 and -45 deg
wedge_pts = [
    (CENTER, CENTER),
    (CENTER + 1000 * math.cos(math.radians(-138)), CENTER + 1000 * math.sin(math.radians(-138))),
    (CENTER + 1000 * math.cos(math.radians(-90)), CENTER - 1100),
    (CENTER + 1000 * math.cos(math.radians(-42)), CENTER + 1000 * math.sin(math.radians(-42)))
]
d_cut.polygon(wedge_pts, fill=0)

# Multiply ring mask by cut mask
mask_rings_open = ImageChops = ImageOps.invert(mask_cut)
# Cut opening in rings
for y in range(SIZE):
    for x in range(SIZE):
        if mask_cut.getpixel((x, y)) == 0:
            mask_rings.putpixel((x, y), 0)

# Paste white rings onto fb
fb.paste(Image.new('RGBA', (SIZE, SIZE), (HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255)), mask=mask_rings)

# Add decorative traditional terminals at the open edges of the rings
draw_fb = ImageDraw.Draw(fb)
for ang in [-138, -42]:
    rad = math.radians(ang)
    # Outer ring terminal
    ox = CENTER + R_out * math.cos(rad)
    oy = CENTER + R_out * math.sin(rad)
    draw_fb.ellipse((ox - 15, oy - 15, ox + 15, oy + 15), fill=HERMES_WHITE)
    # Inner ring terminal
    ix = CENTER + R_in * math.cos(rad)
    iy = CENTER + R_in * math.sin(rad)
    draw_fb.ellipse((ix - 10, iy - 10, ix + 10, iy + 10), fill=HERMES_WHITE)
    # Connecting bar between terminals
    draw_fb.line([(ox, oy), (ix, iy)], fill=HERMES_WHITE, width=8)

# Now composite the portrait so face is clipped inside circular field,
# BUT the horns layer on top are UNCLIPPED, breaking through the gap!
clip_body = Image.new('L', (SIZE, SIZE), 0)
d_cbody = ImageDraw.Draw(clip_body)
d_cbody.ellipse((CENTER - R_in, CENTER - R_in, CENTER + R_in, CENTER + R_in), fill=255)
# Add open top wedge to clipping so horns stay visible above R_in
d_cbody.polygon(wedge_pts, fill=255)

fb.paste(portrait_large, mask=clip_body)

# Add cardinal notch diamond studs at Left, Right, Bottom
for ang in [0, 90, 180]:
    rad = math.radians(ang)
    dx = CENTER + R_out * math.cos(rad)
    dy = CENTER + R_out * math.sin(rad)
    d_sz = 26
    draw_fb.polygon([(dx, dy - d_sz), (dx + d_sz, dy), (dx, dy + d_sz), (dx - d_sz, dy)], fill=HERMES_WHITE)
    draw_fb.ellipse((dx - 5, dy - 5, dx + 5, dy + 5), fill=HERMES_BLUE)

# Place traditional crimson Hanko seal in lower right
hanko_fb = render_hanko(size=280, text="零弐", is_kanji=True)
fb.paste(hanko_fb, (1320, 1320), hanko_fb)

fb_1k = fb.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
fb_1k.save('assets/zerotwo_traditional_framebreaker_crest.png')
print("Design 5 saved.")

# -------------------------------------------------------------------------
# DESIGN 6: Traditional Kikkō-mon (Sacred Hexagonal Crest)
# -------------------------------------------------------------------------
print("Generating Design 6: Traditional Kikkō Hexagonal Crest...")
kikko = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

def get_hex_points(cx, cy, radius, rot_deg=0):
    pts = []
    for i in range(6):
        ang = math.radians(rot_deg + i * 60)
        pts.append((cx + radius * math.cos(ang), cy + radius * math.sin(ang)))
    return pts

# Inner hex clip (R=820, rotated 30 deg so flat edges are top/bottom or pointy top)
# rot_deg = 30 gives pointy top/bottom; rot_deg = 0 gives flat top/bottom.
# In Japanese Kikkō, rot_deg = 30 (pointy top) is standard!
hex_clip = Image.new('L', (SIZE, SIZE), 0)
d_hex_c = ImageDraw.Draw(hex_clip)
d_hex_c.polygon(get_hex_points(CENTER, CENTER, 815, 30), fill=255)
kikko.paste(portrait_layer, mask=hex_clip)

draw_kikko = ImageDraw.Draw(kikko)
# Outer hex: R=885, stroke=24
pts_out = get_hex_points(CENTER, CENTER, 885, 30)
for i in range(6):
    draw_kikko.line([pts_out[i], pts_out[(i+1)%6]], fill=HERMES_WHITE, width=24)

# Inner hex: R=828, stroke=12
pts_in = get_hex_points(CENTER, CENTER, 828, 30)
for i in range(6):
    draw_kikko.line([pts_in[i], pts_in[(i+1)%6]], fill=HERMES_WHITE, width=12)

# Japanese Mitsuba / Cloverleaf ornaments at the 6 vertices
for p in pts_out:
    # Stylized traditional corner trefoil
    draw_kikko.ellipse((p[0] - 16, p[1] - 16, p[0] + 16, p[1] + 16), fill=HERMES_WHITE)
    draw_kikko.ellipse((p[0] - 6, p[1] - 6, p[0] + 6, p[1] + 6), fill=HERMES_BLUE)

# Place Hanko seal at lower corner
hanko_kikko = render_hanko(size=250, text="02", is_kanji=False)
kikko.paste(hanko_kikko, (CENTER - 125, CENTER + 540), hanko_kikko)

kikko_1k = kikko.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
kikko_1k.save('assets/zerotwo_traditional_kikko_hexagon.png')
print("Design 6 saved.")

# -------------------------------------------------------------------------
# DESIGN 7: Classical Hermes Victor's Laurel Medallion
# -------------------------------------------------------------------------
print("Generating Design 7: Classical Laurel Medallion...")
laurel_img = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Inner medallion clip
clip_l = Image.new('L', (SIZE, SIZE), 0)
d_cl = ImageDraw.Draw(clip_l)
d_cl.ellipse((CENTER - 690, CENTER - 690, CENTER + 690, CENTER + 690), fill=255)

SCALE_L = 0.72
LW = int(SIZE * SCALE_L)
LH = int(SIZE * SCALE_L)
stencil_l = stencil_src.resize((LW, LH), Image.Resampling.LANCZOS)
portrait_l = Image.new('RGB', (SIZE, SIZE), HERMES_BLUE)
portrait_l.paste(stencil_l, ((SIZE - LW)//2, (SIZE - LH)//2 + 40))
laurel_img.paste(portrait_l, mask=clip_l)

draw_l = ImageDraw.Draw(laurel_img)

# Concentric inner rim
draw_l.ellipse((CENTER - 700, CENTER - 700, CENTER + 700, CENTER + 700), outline=HERMES_WHITE, width=12)
draw_l.ellipse((CENTER - 725, CENTER - 725, CENTER + 725, CENTER + 725), outline=HERMES_WHITE, width=6)

# Procedural Classical Laurel Wreath around circumference (from 80 deg to -80 deg on both sides)
# Left branch
def draw_leaf(draw_obj, cx, cy, length, width, angle_deg, fill_c):
    # Elliptical rotated leaf
    patch = Image.new('RGBA', (int(length*2), int(length*2)), (0, 0, 0, 0))
    dp = ImageDraw.Draw(patch)
    dp.ellipse((int(length*0.5), int(length - width/2), int(length*1.5), int(length + width/2)), fill=fill_c)
    patch_rot = patch.rotate(angle_deg, resample=Image.Resampling.BICUBIC)
    px = int(cx - length)
    py = int(cy - length)
    draw_obj._image.paste(patch_rot, (px, py), patch_rot)

# Draw laurel leaves along track R=820
R_wreath = 825
num_leaves = 15
# Left wreath branch (from 85 deg down to -85 deg up)
for i in range(num_leaves):
    t = i / (num_leaves - 1)
    # angle in degrees: starts at 75 (bottom left), curves up to -75 (top left)
    ang = 80 - 155 * t
    rad = math.radians(ang)
    stem_x = CENTER - R_wreath * math.cos(rad)
    stem_y = CENTER + R_wreath * math.sin(rad)
    
    # Pair of leaves at each step
    # Leaf 1 pointing along curve
    leaf_ang = 90 - ang + 25
    draw_leaf(draw_l, stem_x, stem_y, length=55, width=22, angle_deg=leaf_ang, fill_c=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
    # Leaf 2 pointing outward
    draw_leaf(draw_l, stem_x, stem_y, length=48, width=18, angle_deg=leaf_ang - 45, fill_c=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))

# Right wreath branch
for i in range(num_leaves):
    t = i / (num_leaves - 1)
    ang = 80 - 155 * t
    rad = math.radians(ang)
    stem_x = CENTER + R_wreath * math.cos(rad)
    stem_y = CENTER + R_wreath * math.sin(rad)
    
    leaf_ang = -(90 - ang + 25)
    draw_leaf(draw_l, stem_x, stem_y, length=55, width=22, angle_deg=leaf_ang, fill_c=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))
    draw_leaf(draw_l, stem_x, stem_y, length=48, width=18, angle_deg=leaf_ang + 45, fill_c=(HERMES_WHITE[0], HERMES_WHITE[1], HERMES_WHITE[2], 255))

# Top Victor's Star
def draw_star(draw_obj, cx, cy, r_out, r_in, fill_c):
    pts = []
    for i in range(10):
        r = r_out if i % 2 == 0 else r_in
        ang = math.radians(-90 + i * 36)
        pts.append((cx + r * math.cos(ang), cy + r * math.sin(ang)))
    draw_obj.polygon(pts, fill=fill_c)

draw_star(draw_l, CENTER, CENTER - 835, r_out=32, r_in=14, fill_c=HERMES_WHITE)

# Bottom ribbon tie / rosette
draw_l.ellipse((CENTER - 25, CENTER + 830 - 25, CENTER + 25, CENTER + 830 + 25), fill=CRIMSON_RED)
draw_l.ellipse((CENTER - 15, CENTER + 830 - 15, CENTER + 15, CENTER + 830 + 15), outline=HERMES_WHITE, width=4)

laurel_1k = laurel_img.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
laurel_1k.save('assets/zerotwo_traditional_laurel_wreath.png')
print("Design 7 saved.")

# -------------------------------------------------------------------------
# DESIGN 8: Traditional Shinto Torii Shrine Arch Cartouche
# -------------------------------------------------------------------------
print("Generating Design 8: Shinto Shrine Cartouche...")
torii = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Arched portal clipping mask for Zero Two
clip_torii = Image.new('L', (SIZE, SIZE), 0)
d_ctorii = ImageDraw.Draw(clip_torii)
# Round top arch from x=400 to 1648, y=420 to 1750
d_ctorii.rounded_rectangle((380, 420, 1668, 1780), radius=380, fill=255)
torii.paste(portrait_layer, mask=clip_torii)

draw_torii = ImageDraw.Draw(torii)

# 1. Upper Kasagi Lintel (Top curving beam with flared ends)
# Top curve
kasagi_pts = [
    (180, 310), (CENTER, 280), (SIZE - 180, 310),
    (SIZE - 160, 240), (CENTER, 210), (160, 240)
]
draw_torii.polygon(kasagi_pts, fill=HERMES_WHITE)
# 2. Lower Shimaki & Nuki horizontal crossbeams
draw_torii.rectangle((260, 340, SIZE - 260, 390), fill=HERMES_WHITE)
draw_torii.rectangle((290, 470, SIZE - 290, 510), fill=HERMES_WHITE)

# 3. Two vertical Hashira pillars flanking the arched portal
draw_torii.rectangle((300, 390, 370, 1850), fill=HERMES_WHITE)
draw_torii.rectangle((SIZE - 370, 390, SIZE - 300, 1850), fill=HERMES_WHITE)

# Pillar base pedestals (Daiishi)
draw_torii.rectangle((270, 1820, 400, 1890), fill=HERMES_WHITE)
draw_torii.rectangle((SIZE - 400, 1820, SIZE - 270, 1890), fill=HERMES_WHITE)

# Arch portal border line
draw_torii.rounded_rectangle((380, 420, 1668, 1780), radius=380, outline=HERMES_WHITE, width=16)

# Central Sacred Gaku / Plaque at top center of Torii
draw_torii.rounded_rectangle((CENTER - 110, 360, CENTER + 110, 490), radius=16, fill=CRIMSON_RED, outline=HERMES_WHITE, width=6)
# Text "02" on plaque
font_torii = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 64)
bb_t = draw_torii.textbbox((0, 0), "02", font=font_torii)
draw_torii.text(((SIZE - (bb_t[2]-bb_t[0]))//2, 385), "02", font=font_torii, fill=HERMES_WHITE)

torii_1k = torii.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
torii_1k.save('assets/zerotwo_traditional_torii_shrine_arch.png')
print("Design 8 saved.")

# -------------------------------------------------------------------------
# DESIGN 9: Modernist Brutalist Cyber-Mon (Hermes x ZeroTwo Stencil)
# -------------------------------------------------------------------------
print("Generating Design 9: Modernist Brutalist Cyber-Mon...")
brut = Image.new('RGBA', (SIZE, SIZE), (HERMES_BLUE[0], HERMES_BLUE[1], HERMES_BLUE[2], 255))

# Outer Chamfered Octagonal / Diamond Crest
chamfer = 180
R_b = 890
brut_pts = [
    (CENTER - R_b + chamfer, CENTER - R_b),
    (CENTER + R_b - chamfer, CENTER - R_b),
    (CENTER + R_b, CENTER - R_b + chamfer),
    (CENTER + R_b, CENTER + R_b - chamfer),
    (CENTER + R_b - chamfer, CENTER + R_b),
    (CENTER - R_b + chamfer, CENTER + R_b),
    (CENTER - R_b, CENTER + R_b - chamfer),
    (CENTER - R_b, CENTER - R_b + chamfer),
]

clip_brut = Image.new('L', (SIZE, SIZE), 0)
d_cbrut = ImageDraw.Draw(clip_brut)
d_cbrut.polygon(brut_pts, fill=255)
brut.paste(portrait_layer, mask=clip_brut)

draw_brut = ImageDraw.Draw(brut)
for i in range(8):
    draw_brut.line([brut_pts[i], brut_pts[(i+1)%8]], fill=HERMES_WHITE, width=24)

# Inset inner chamfered border
R_bi = 840
brut_pts_in = [
    (CENTER - R_bi + chamfer*0.9, CENTER - R_bi),
    (CENTER + R_bi - chamfer*0.9, CENTER - R_bi),
    (CENTER + R_bi, CENTER - R_bi + chamfer*0.9),
    (CENTER + R_bi, CENTER + R_bi - chamfer*0.9),
    (CENTER + R_bi - chamfer*0.9, CENTER + R_bi),
    (CENTER - R_bi + chamfer*0.9, CENTER + R_bi),
    (CENTER - R_bi, CENTER + R_bi - chamfer*0.9),
    (CENTER - R_bi, CENTER - R_bi + chamfer*0.9),
]
for i in range(8):
    draw_brut.line([brut_pts_in[i], brut_pts_in[(i+1)%8]], fill=HERMES_WHITE, width=10)

# Left flank vertical crimson seal bar with white kanji
bar_w = 120
bar_h = 580
bar_x = CENTER - R_bi + 60
bar_y = CENTER - bar_h // 2
draw_brut.rounded_rectangle((bar_x, bar_y, bar_x + bar_w, bar_y + bar_h), radius=20, fill=CRIMSON_RED, outline=HERMES_WHITE, width=6)
# Kanji 零 and 弐
draw_brut.text((bar_x + 18, bar_y + 40), "零", font=cjk_font_med, fill=HERMES_WHITE)
draw_brut.text((bar_x + 18, bar_y + 190), "弐", font=cjk_font_med, fill=HERMES_WHITE)
# Numeral 02 below
draw_brut.text((bar_x + 22, bar_y + 360), "02", font=ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 60), fill=HERMES_WHITE)

# Right flank technical serial marks
font_tech = ImageFont.truetype("/usr/share/fonts/truetype/dejavu/DejaVuSans-Bold.ttf", 38)
draw_brut.text((CENTER + R_bi - 220, CENTER - 180), "SPEC // 002", font=font_tech, fill=HERMES_WHITE)
draw_brut.text((CENTER + R_bi - 220, CENTER - 120), "FRANXX // STENCIL", font=font_tech, fill=HERMES_WHITE)
draw_brut.text((CENTER + R_bi - 220, CENTER - 60), "STATUS // RELEASE", font=font_tech, fill=HERMES_WHITE)

brut_1k = brut.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
brut_1k.save('assets/zerotwo_traditional_brutalist_seal.png')
print("Design 9 saved.")

print("All new designs generated successfully!")
