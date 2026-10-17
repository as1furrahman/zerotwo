import math
import os
from PIL import Image, ImageDraw, ImageFont

HERMES_WHITE = (245, 245, 245)    # #F5F5F5
CRIMSON_RED = (255, 23, 68)       # #FF1744

# Defined Grey Palettes
GREY_GRAPHITE = (24, 24, 28)       # #18181C - Deep Matte Graphite / Charcoal
GREY_SLATE = (40, 44, 52)          # #282C34 - Cool Industrial Slate Grey
GREY_TITANIUM = (55, 60, 70)       # #373C46 - Mid-Tone Titanium / Steel Grey
GREY_SMOKE = (30, 32, 38)          # #1E2026 - Dark Gunmetal Smoke

SIZE = 2048
CENTER = SIZE // 2

# Load raw stencil
stencil_src = Image.open('assets/zerotwo_face_electric_blue_stencil.png').convert('RGB')
w_src, h_src = stencil_src.size
pix_src = stencil_src.load()

# Separate into clean binary masks (1024x1024)
mask_white_1k = Image.new('L', (w_src, h_src), 0)
mask_horns_1k = Image.new('L', (w_src, h_src), 0)
mw_pix = mask_white_1k.load()
mh_pix = mask_horns_1k.load()

for y in range(h_src):
    for x in range(w_src):
        p = pix_src[x, y]
        if p == (245, 245, 245):
            mw_pix[x, y] = 255
        elif p == (255, 23, 68):
            mh_pix[x, y] = 255

print(f"Masks extracted from raw stencil: white={sum(1 for y in range(h_src) for x in range(w_src) if mw_pix[x,y]==255)}, horns={sum(1 for y in range(h_src) for x in range(w_src) if mh_pix[x,y]==255)}")

# Function to render grey arch cartouche
def render_cartouche_in_color(bg_color, name_suffix, make_transparent=True):
    # 1. Scale masks to 2048
    SCALED_W = int(SIZE * 0.86)
    SCALED_H = int(SIZE * 0.86)
    
    mw_scaled = mask_white_1k.resize((SCALED_W, SCALED_H), Image.Resampling.LANCZOS)
    mh_scaled = mask_horns_1k.resize((SCALED_W, SCALED_H), Image.Resampling.LANCZOS)
    
    # 2. Build 2048 portrait layer in requested grey
    portrait = Image.new('RGB', (SIZE, SIZE), bg_color)
    # White face & headband
    white_layer = Image.new('RGB', (SIZE, SIZE), HERMES_WHITE)
    horns_layer = Image.new('RGB', (SIZE, SIZE), CRIMSON_RED)
    
    # Position matches exact centered arch
    pos_x = (SIZE - SCALED_W) // 2
    pos_y = 117
    
    # Create full canvas masks
    full_mask_w = Image.new('L', (SIZE, SIZE), 0)
    full_mask_w.paste(mw_scaled, (pos_x, pos_y))
    full_mask_h = Image.new('L', (SIZE, SIZE), 0)
    full_mask_h.paste(mh_scaled, (pos_x, pos_y))
    
    portrait.paste(white_layer, mask=full_mask_w)
    portrait.paste(horns_layer, mask=full_mask_h)
    
    # 3. Arch Cartouche geometry
    x0, x1 = 380, 1668
    y0, y1 = 344, 1704
    rad = 380
    
    # Canvas
    cartouche_img = Image.new('RGBA', (SIZE, SIZE), (bg_color[0], bg_color[1], bg_color[2], 255))
    
    # Arch inner clipping mask
    clip = Image.new('L', (SIZE, SIZE), 0)
    d_c = ImageDraw.Draw(clip)
    d_c.rounded_rectangle((x0, y0, x1, y1), radius=rad, fill=255)
    
    cartouche_img.paste(portrait, mask=clip)
    
    # Draw single precision white arch border (width = 18)
    draw = ImageDraw.Draw(cartouche_img)
    draw.rounded_rectangle((x0, y0, x1, y1), radius=rad, outline=HERMES_WHITE, width=18)
    
    # Save solid version
    solid_1k = cartouche_img.convert('RGB').resize((1024, 1024), Image.Resampling.LANCZOS)
    solid_path = f'assets/zerotwo_arch_cartouche_{name_suffix}_solid.png'
    solid_1k.save(solid_path)
    print(f"Saved solid: {solid_path}")
    
    # Transparent cutout version
    if make_transparent:
        trans_img = cartouche_img.copy()
        mask_trans = Image.new('L', (SIZE, SIZE), 0)
        d_mt = ImageDraw.Draw(mask_trans)
        d_mt.rounded_rectangle((x0 - 4, y0 - 4, x1 + 4, y1 + 4), radius=rad + 4, fill=255)
        trans_img.putalpha(mask_trans)
        trans_1k = trans_img.resize((1024, 1024), Image.Resampling.LANCZOS)
        trans_path = f'assets/zerotwo_arch_cartouche_{name_suffix}_transparent.png'
        trans_1k.save(trans_path)
        print(f"Saved transparent: {trans_path}")
        return trans_path
    return solid_path

# Render the Grey Variations:
# 1. Graphite / Charcoal Grey (Darkest, highest contrast)
render_cartouche_in_color(GREY_GRAPHITE, 'grey_graphite')

# 2. Slate Grey (Cool industrial grey, classic techwear)
render_cartouche_in_color(GREY_SLATE, 'grey_slate')

# 3. Titanium / Steel Grey (Mid-tone grey, distinct grey visibility)
render_cartouche_in_color(GREY_TITANIUM, 'grey_titanium')

# 4. Gunmetal Smoke Grey
render_cartouche_in_color(GREY_SMOKE, 'grey_smoke')

print("All grey variations rendered successfully.")
