"""Render the market-data-platform explainer clip with pycairo + ffmpeg.

requirements: python3, pycairo (pip install pycairo), ffmpeg, the Inter font.

usage: python3 render.py preview      # PNG stills of key moments -> out/
       python3 render.py render       # scene MP4s + final concat  -> out/
"""
import math, os, random, subprocess, sys
import cairo

W, H, FPS = 1920, 1080, 30
OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "out")
os.makedirs(OUT, exist_ok=True)

BG = (0.043, 0.071, 0.125)
PANEL = (0.086, 0.122, 0.192)
EDGE = (0.20, 0.26, 0.36)
TXT = (0.93, 0.95, 0.98)
MUTED = (0.58, 0.64, 0.74)
CB = (0.23, 0.51, 0.96)   # coinbase blue
KR = (0.55, 0.36, 0.96)   # kraken purple
GO = (0.13, 0.78, 0.86)   # cyan
KAF = (0.95, 0.55, 0.25)  # orange
CH = (0.98, 0.80, 0.20)   # yellow
GRA = (0.30, 0.85, 0.50)  # green
RED = (0.95, 0.30, 0.30)

def hexa(c, a=1.0):
    return (*c, a)

# ---------------------------------------------------------------- layout
NODES = {
    "cb":  (110, 300, 240, 120),
    "kr":  (110, 570, 240, 120),
    "rec": (520, 410, 270, 170),
    "k0":  (930, 270, 230, 100),
    "k1":  (930, 445, 230, 100),
    "k2":  (930, 620, 230, 100),
    "c0":  (1310, 330, 240, 130),
    "c1":  (1310, 530, 240, 130),
    "gr":  (1660, 360, 200, 270),
}

def center(n):
    x, y, w, h = NODES[n]
    return x + w / 2, y + h / 2

def left(n):
    x, y, w, h = NODES[n]
    return x, y + h / 2

def right(n):
    x, y, w, h = NODES[n]
    return x + w, y + h / 2

# ---------------------------------------------------------------- timeline (main section, seconds)
SCENES = [  # (start, end, step label, caption, stage revealed)
    (0, 7, "1 / 6   Exchanges",
     "Crypto exchanges stream every trade and order-book change, live.\nHundreds of messages per second, for just three coins.", 1),
    (7, 14.5, "2 / 6   Ingest",
     "Go programs catch every message, stamp the exact time it arrived,\nand reconnect on their own when a feed drops.", 2),
    (14.5, 21.5, "3 / 6   Kafka",
     "Kafka writes every message to three servers.\nIf one of them dies, nothing is lost.", 3),
    (21.5, 28.5, "4 / 6   ClickHouse",
     "ClickHouse turns the stream into a database that answers\nquestions in milliseconds, with a second copy kept in sync.", 4),
    (28.5, 34.5, "5 / 6   Dashboards",
     "Dashboards turn raw data into prices, volume and volatility,\nminute by minute.", 5),
    (34.5, 44, "6 / 6   Break it on purpose",
     "Then we break it on purpose: kill servers, cut the network,\nand measure how fast it recovers and what was lost.", 5),
]
MAIN_LEN = SCENES[-1][1]
KR_DOWN = (10.2, 11.8)      # kraken feed drops, recorder reconnects
K_DEAD = 36.5               # broker k1 killed

def stage_at(t):
    s = 0
    for a, b, _, _, st in SCENES:
        if t >= a:
            s = st
    return s

STAGE_T = {1: 0, 2: 7, 3: 14.5, 4: 21.5, 5: 28.5}

def reveal(stage, t, dur=0.6):
    t0 = STAGE_T[stage]
    return max(0.0, min(1.0, (t - t0) / dur))

# ---------------------------------------------------------------- particles
random.seed(7)
SEG = 0.85
particles = []
tt = 0.0
i = 0
while tt < MAIN_LEN:
    src = "cb" if i % 2 == 0 else "kr"
    particles.append((tt, src, random.uniform(-14, 14)))
    tt += random.uniform(0.09, 0.16)
    i += 1

def lerp(a, b, u):
    return a[0] + (b[0] - a[0]) * u, a[1] + (b[1] - a[1]) * u

def ease(u):
    return u * u * (3 - 2 * u)

def alive_brokers(t):
    return ["k0", "k2"] if t >= K_DEAD else ["k0", "k1", "k2"]

def draw_particles(cr, t):
    st = stage_at(t)
    for birth, src, jit in particles:
        age = t - birth
        if age < 0 or age > SEG * 5:
            continue
        if src == "kr" and KR_DOWN[0] <= birth < KR_DOWN[1]:
            continue
        col = CB if src == "cb" else KR
        seg = int(age // SEG)
        u = ease((age % SEG) / SEG)
        # segment start time decides whether the target stage existed
        seg_start = birth + seg * SEG
        need = {0: 2, 1: 3, 2: 4, 3: 5}.get(seg, 99)
        if seg == 0:
            a = right(src); a = (a[0], a[1] + jit * 0.4)
            if stage_at(seg_start) < 2:
                # before ingest exists: dots drift out and fade
                b = (a[0] + 140, a[1] + jit)
                dot(cr, lerp(a, b, u), col, 1 - u)
            else:
                b = left("rec"); b = (b[0], b[1] + jit)
                dot(cr, lerp(a, b, u), col, 1)
        elif seg == 1:
            if stage_at(seg_start) < 3:
                continue
            a = right("rec")
            for k in alive_brokers(seg_start):
                b = left(k)
                dot(cr, lerp(a, b, u), col, 1, r=5)
        elif seg == 2:
            if stage_at(seg_start) < 4:
                continue
            k = alive_brokers(seg_start)[0]
            target = "c0" if int(birth * 10) % 2 == 0 else "c1"
            a = right(k); b = left(target)
            dot(cr, lerp(a, b, u), col, 1, r=5)
        elif seg == 3:
            if stage_at(seg_start) < 5:
                continue
            target = "c0" if int(birth * 10) % 2 == 0 else "c1"
            a = right(target); b = left("gr")
            dot(cr, lerp(a, b, u), col, 0.9, r=4)

def dot(cr, p, col, a, r=6):
    if a <= 0:
        return
    cr.set_source_rgba(*col, 0.25 * a)
    cr.arc(p[0], p[1], r * 2.2, 0, 2 * math.pi); cr.fill()
    cr.set_source_rgba(*col, a)
    cr.arc(p[0], p[1], r, 0, 2 * math.pi); cr.fill()

# ---------------------------------------------------------------- drawing helpers
def font(cr, size, weight="Regular"):
    w = cairo.FONT_WEIGHT_BOLD if weight in ("Bold", "SemiBold") else cairo.FONT_WEIGHT_NORMAL
    name = "Inter SemiBold" if weight == "SemiBold" else "Inter"
    cr.select_font_face(name, cairo.FONT_SLANT_NORMAL, w)
    cr.set_font_size(size)

def text(cr, s, x, y, size, col=TXT, a=1.0, weight="Regular", anchor="left"):
    font(cr, size, weight)
    ext = cr.text_extents(s)
    if anchor == "center":
        x -= ext.width / 2 + ext.x_bearing
    elif anchor == "right":
        x -= ext.width + ext.x_bearing
    cr.set_source_rgba(*col, a)
    cr.move_to(x, y)
    cr.show_text(s)

def rrect(cr, x, y, w, h, r):
    cr.new_sub_path()
    cr.arc(x + w - r, y + r, r, -math.pi / 2, 0)
    cr.arc(x + w - r, y + h - r, r, 0, math.pi / 2)
    cr.arc(x + r, y + h - r, r, math.pi / 2, math.pi)
    cr.arc(x + r, y + r, r, math.pi, 3 * math.pi / 2)
    cr.close_path()

def node(cr, n, title, sub, col, a, glow=0.0, dead=False, top=False):
    if a <= 0:
        return
    x, y, w, h = NODES[n]
    if glow > 0:
        cr.set_source_rgba(*col, 0.18 * glow * a)
        rrect(cr, x - 8, y - 8, w + 16, h + 16, 22); cr.fill()
    rrect(cr, x, y, w, h, 16)
    cr.set_source_rgba(*PANEL, a); cr.fill_preserve()
    edge = RED if dead else col
    cr.set_source_rgba(*edge, a); cr.set_line_width(3); cr.stroke()
    # accent bar
    rrect(cr, x + 14, y + 16, 8, h - 32, 4)
    cr.set_source_rgba(*edge, a); cr.fill()
    cx = x + 40
    ty = y + 52 if top else y + h / 2 - 4
    text(cr, title, cx, ty, 26, TXT if not dead else MUTED, a, "SemiBold")
    if sub:
        text(cr, sub, cx, y + h / 2 + 28, 19, MUTED, a)
    if dead:
        cr.set_source_rgba(*RED, a); cr.set_line_width(6)
        mx, my = x + w - 40, y + h / 2
        cr.move_to(mx - 16, my - 16); cr.line_to(mx + 16, my + 16)
        cr.move_to(mx + 16, my - 16); cr.line_to(mx - 16, my + 16); cr.stroke()

def link(cr, a, b, col, alpha, dashed=False, width=2):
    if alpha <= 0:
        return
    cr.set_source_rgba(*col, alpha)
    cr.set_line_width(width)
    if dashed:
        cr.set_dash([10, 8])
    cr.move_to(*a); cr.line_to(*b); cr.stroke()
    cr.set_dash([])

def group_label(cr, s, x, y, col, a):
    text(cr, s, x, y, 22, col, a, "SemiBold")

def caption(cr, s, a):
    lines = s.split("\n")
    y0 = 905
    rrect(cr, 160, 862, W - 320, 150, 20)
    cr.set_source_rgba(0, 0, 0, 0.35 * a); cr.fill()
    for i, ln in enumerate(lines):
        text(cr, ln, W / 2, y0 + 20 + i * 46, 34, TXT, a, anchor="center")

# ---------------------------------------------------------------- main frame
def main_frame(cr, t):
    cr.set_source_rgb(*BG); cr.paint()
    # current scene
    sc = SCENES[0]
    for s in SCENES:
        if t >= s[0]:
            sc = s
    a0, b0, label, cap, _ = sc
    fade = min(1.0, (t - a0) / 0.45, (b0 - t) / 0.45 + 0.0) if sc is not SCENES[-1] else min(1.0, (t - a0) / 0.45)
    fade = max(0.0, fade)
    text(cr, "market-data-platform", 110, 120, 26, MUTED, 1, "SemiBold")
    text(cr, label, W - 110, 120, 30, TXT, fade, "SemiBold", anchor="right")
    # progress bar
    cr.set_source_rgba(*EDGE, 1); cr.rectangle(110, 150, W - 220, 4); cr.fill()
    cr.set_source_rgba(*GO, 1); cr.rectangle(110, 150, (W - 220) * t / MAIN_LEN, 4); cr.fill()

    st = stage_at(t)
    r1 = reveal(1, t); r2 = reveal(2, t); r3 = reveal(3, t); r4 = reveal(4, t); r5 = reveal(5, t)
    def focus(stage):
        return 1.0 if sc[4] == stage and sc is not SCENES[-1] else 0.0

    # links
    kr_down = KR_DOWN[0] <= t < KR_DOWN[1]
    if st >= 2:
        link(cr, right("cb"), left("rec"), EDGE, r2)
        link(cr, right("kr"), left("rec"), RED if kr_down else EDGE, r2, dashed=kr_down, width=3 if kr_down else 2)
    if st >= 3:
        for k in ("k0", "k1", "k2"):
            dead = k == "k1" and t >= K_DEAD
            link(cr, right("rec"), left(k), RED if dead else EDGE, r3, dashed=dead)
    if st >= 4:
        for k in ("k0", "k2"):
            for c in ("c0", "c1"):
                link(cr, right(k), left(c), EDGE, r4 * 0.6)
        # replica sync
        p0 = (center("c0")[0] + 60, NODES["c0"][1] + NODES["c0"][3])
        p1 = (center("c1")[0] + 60, NODES["c1"][1])
        link(cr, p0, p1, CH, r4 * 0.8, dashed=True, width=3)
        text(cr, "sync", p0[0] + 12, (p0[1] + p1[1]) / 2 + 7, 18, CH, r4)
    if st >= 5:
        for c in ("c0", "c1"):
            link(cr, right(c), left("gr"), EDGE, r5)

    draw_particles(cr, t)

    # nodes
    node(cr, "cb", "Coinbase", "trades · order book", CB, r1, focus(1))
    node(cr, "kr", "Kraken", "trades · order book", KR, r1, focus(1))
    if st >= 2:
        node(cr, "rec", "Go ingestors", "timestamp · reconnect", GO, r2, focus(2))
        if kr_down:
            text(cr, "feed dropped · reconnecting…", right("kr")[0] + 20, right("kr")[1] + 50, 20, RED, 1, "SemiBold")
        elif KR_DOWN[1] <= t < KR_DOWN[1] + 1.4:
            text(cr, "reconnected", right("kr")[0] + 20, right("kr")[1] + 50, 20, GRA, 1, "SemiBold")
    if st >= 3:
        group_label(cr, "Kafka · 3 copies", NODES["k0"][0], NODES["k0"][1] - 22, KAF, r3)
        for i, k in enumerate(("k0", "k1", "k2")):
            dead = k == "k1" and t >= K_DEAD
            node(cr, k, f"broker {i + 1}", "killed" if dead else "copy of every msg", KAF, r3, focus(3), dead=dead)
    if st >= 4:
        group_label(cr, "ClickHouse · 2 replicas", NODES["c0"][0], NODES["c0"][1] - 22, CH, r4)
        for i, c in enumerate(("c0", "c1")):
            node(cr, c, f"replica {i + 1}", "", CH, r4, focus(4))
            rows(cr, c, t, r4)
    if st >= 5:
        node(cr, "gr", "Grafana", "", GRA, r5, focus(5), top=True)
        chart(cr, t, r5)

    # chaos overlay
    if t >= K_DEAD - 0.3:
        u = max(0.0, min(1.0, (t - K_DEAD) / 0.5))
        bx, by = 520, 735
        rrect(cr, bx, by, 520, 92, 16)
        cr.set_source_rgba(*PANEL, u); cr.fill_preserve()
        cr.set_source_rgba(*GRA, u); cr.set_line_width(2); cr.stroke()
        text(cr, "Goal: 0 messages lost", bx + 24, by + 40, 28, GRA, u, "SemiBold")
        text(cr, "target, measured in the chaos tests", bx + 24, by + 72, 19, MUTED, u)
        # flash on kill
        fl = max(0.0, 1 - (t - K_DEAD) / 0.6)
        if fl > 0:
            x, y, w, h = NODES["k1"]
            cr.set_source_rgba(*RED, 0.35 * fl)
            rrect(cr, x - 14, y - 14, w + 28, h + 28, 24); cr.fill()

    caption(cr, cap, fade)

def rows(cr, c, t, a):
    x, y, w, h = NODES[c]
    n = int(max(0, (t - STAGE_T[4] - 0.4) * 2.2))
    n = min(n, 5)
    for i in range(n):
        ln = 120 - (i * 37) % 50
        cr.set_source_rgba(*CH, 0.55 * a)
        rrect(cr, x + 40, y + 74 + i * 9, ln, 5, 2); cr.fill()

def chart(cr, t, a):
    x, y, w, h = NODES["gr"]
    px, py, pw, ph = x + 40, y + 110, w - 64, h - 140
    text(cr, "1-min price", x + 40, y + 82, 18, MUTED, a)
    random.seed(3)
    price = 0.5
    n = 12
    shown = int(max(0, (t - STAGE_T[5] - 0.3) * 2.0)) if t >= STAGE_T[5] else 0
    shown = min(shown, n)
    cw = pw / n
    for i in range(n):
        o = price
        price = max(0.12, min(0.88, price + random.uniform(-0.16, 0.18)))
        c = price
        hi = max(o, c) + random.uniform(0.02, 0.08)
        lo = min(o, c) - random.uniform(0.02, 0.08)
        if i >= shown:
            continue
        col = GRA if c >= o else RED
        cx = px + cw * i + cw / 2
        cr.set_source_rgba(*col, a); cr.set_line_width(2)
        cr.move_to(cx, py + ph * (1 - hi)); cr.line_to(cx, py + ph * (1 - lo)); cr.stroke()
        top, bot = py + ph * (1 - max(o, c)), py + ph * (1 - min(o, c))
        cr.rectangle(cx - cw * 0.3, top, cw * 0.6, max(3, bot - top)); cr.fill()

# ---------------------------------------------------------------- title / end
def title_frame(cr, t, dur):
    cr.set_source_rgb(*BG); cr.paint()
    a = min(1.0, t / 0.6, (dur - t) / 0.5)
    a = max(0.0, a)
    text(cr, "From exchange to dashboard", W / 2, 470, 84, TXT, a, "SemiBold", anchor="center")
    text(cr, "a real-time market-data platform, built from scratch", W / 2, 550, 38, MUTED, a, anchor="center")
    # little flowing dots
    for i in range(14):
        u = ((t * 0.35 + i / 14) % 1.0)
        xx = 560 + 800 * u
        dot(cr, (xx, 640), CB if i % 2 else KR, a * math.sin(math.pi * u), r=5)
    text(cr, "market-data-platform  ·  Vitaliy Pikalo", W / 2, 760, 26, MUTED, a * 0.9, anchor="center")

def end_frame(cr, t, dur):
    cr.set_source_rgb(*BG); cr.paint()
    a = max(0.0, min(1.0, t / 0.6, (dur - t) / 0.6))
    text(cr, "Designed to run on Kubernetes, set up entirely from code.", W / 2, 300, 50, TXT, a, "SemiBold", anchor="center")
    items_done = ["Exchange recorders (Coinbase, Kraken)", "CI with required checks"]
    items_next = ["Kafka + ClickHouse on Kubernetes", "Replication, backups, restore drills", "Chaos tests: kill it, measure recovery"]
    x0, y0 = 290, 420
    text(cr, "Built", x0, y0, 30, GRA, a, "SemiBold")
    for i, s in enumerate(items_done):
        text(cr, "✓  " + s, x0, y0 + 56 + i * 50, 28, TXT, a)
    x1 = 1000
    text(cr, "Next", x1, y0, 30, KAF, a, "SemiBold")
    for i, s in enumerate(items_next):
        text(cr, "→  " + s, x1, y0 + 56 + i * 50, 28, TXT, a)
    text(cr, "github.com/Vitaliy-Pikalo/market-data-platform", W / 2, 820, 32, GO, a, "SemiBold", anchor="center")

# ---------------------------------------------------------------- output
def surface():
    s = cairo.ImageSurface(cairo.FORMAT_RGB24, W, H)
    return s, cairo.Context(s)

def preview():
    for name, fn, t in [
        ("p_title", "title", 2.0),
        ("p_ingest_drop", "main", 10.8),
        ("p_kafka", "main", 19.0),
        ("p_full", "main", 32.0),
        ("p_chaos", "main", 40.0),
        ("p_end", "end", 3.0),
    ]:
        s, cr = surface()
        if fn == "title":
            title_frame(cr, t, 4.5)
        elif fn == "end":
            end_frame(cr, t, 6.5)
        else:
            main_frame(cr, t)
        s.write_to_png(os.path.join(OUT, name + ".png"))

def encode(name, n_frames, draw):
    tmp = os.path.join(OUT, f"{name}.tmp.mp4")
    final = os.path.join(OUT, f"{name}.mp4")
    p = subprocess.Popen([
        "ffmpeg", "-y", "-loglevel", "error", "-f", "rawvideo", "-pix_fmt", "bgra",
        "-s", f"{W}x{H}", "-r", str(FPS), "-i", "-",
        "-c:v", "libx264", "-preset", "veryfast", "-crf", "18", "-pix_fmt", "yuv420p", tmp,
    ], stdin=subprocess.PIPE)
    s, cr = surface()
    for f in range(n_frames):
        draw(cr, f)
        s.flush()
        p.stdin.write(bytes(s.get_data()))
    p.stdin.close()
    if p.wait() == 0:
        os.replace(tmp, final)
    else:
        raise SystemExit(f"ffmpeg failed for {name}")
    return final

def render():
    files = []
    td, ed = 4.5, 6.5
    files.append(encode("s00_title", int(td * FPS), lambda cr, f: title_frame(cr, f / FPS, td)))
    for i, (a, b, *_rest) in enumerate(SCENES, start=1):
        fa, fb = int(round(a * FPS)), int(round(b * FPS))
        files.append(encode(f"s{i:02d}", fb - fa, lambda cr, f, fa=fa: main_frame(cr, (fa + f) / FPS)))
    files.append(encode("s99_end", int(ed * FPS), lambda cr, f: end_frame(cr, f / FPS, ed)))
    lst = os.path.join(OUT, "list.txt")
    with open(lst, "w") as fh:
        for f in files:
            fh.write(f"file '{f}'\n")
    out = os.path.join(OUT, "market-data-platform-explainer.mp4")
    subprocess.run(["ffmpeg", "-y", "-loglevel", "error", "-f", "concat", "-safe", "0", "-i", lst,
                    "-c", "copy", "-movflags", "+faststart", out], check=True)
    print(out)

if __name__ == "__main__":
    {"preview": preview, "render": render}[sys.argv[1]]()
