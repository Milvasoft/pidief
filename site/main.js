/* pidief site — scroll choreography without libraries.
 *
 * One smoothed scroll position drives every scene, so every element moves with the same inertia.
 * Pinned scenes (.scene > .stage, position: sticky) map their pinned stretch to a progress 0..1.
 * The three pages live in one fixed layer shared by the Render and Operations scenes, so the cut
 * between the two scenes is seamless; the Operations stamp opens a coral wipe that becomes the
 * background of the next scene.
 */
(() => {
  "use strict";

  const RM = matchMedia("(prefers-reduced-motion: reduce)").matches;
  const $ = (s, r = document) => r.querySelector(s);
  const $$ = (s, r = document) => [...r.querySelectorAll(s)];
  const clamp = (v, a = 0, b = 1) => Math.min(b, Math.max(a, v));
  const lerp = (a, b, t) => a + (b - a) * t;
  const seg = (p, a, b) => clamp((p - a) / (b - a));
  const E = {
    o3: (t) => 1 - (1 - t) ** 3,
    o5: (t) => 1 - (1 - t) ** 5,
    io: (t) => (t < 0.5 ? 4 * t * t * t : 1 - (-2 * t + 2) ** 3 / 2),
    i2: (t) => t * t,
    back: (t) => { const c = 1.6; return 1 + (c + 1) * (t - 1) ** 3 + c * (t - 1) ** 2; },
  };
  const bump = (t) => Math.sin(Math.PI * clamp(t)); // 0 → 1 → 0

  // ---------------------------------------------------------------- state
  let W = innerWidth, H = innerHeight, lastW = W;
  let y = scrollY, ys = scrollY, lastFrame = performance.now(), lastKey = "";
  const t0 = performance.now();
  const mobile = () => W <= 820;

  const scenes = {};
  $$(".scene").forEach((el) => { scenes[el.dataset.scene] = { el, vh: +el.dataset.vh, top: 0, len: 1 }; });

  function sizeScenes() {
    for (const s of Object.values(scenes)) s.el.style.height = Math.round((s.vh / 100) * H) + "px";
  }
  function measure() {
    for (const s of Object.values(scenes)) {
      s.top = s.el.getBoundingClientRect().top + scrollY;
      s.len = Math.max(1, s.el.offsetHeight - H);
    }
  }
  const prog = (s) => clamp((ys - s.top) / s.len);
  // progress from the moment the scene enters the viewport until it is fully pinned and done
  const enter = (s) => clamp((ys - (s.top - H)) / (s.len + H));

  // ---------------------------------------------------------------- shared page layer
  const layer = document.createElement("div");
  layer.className = "pages-layer";
  document.body.appendChild(layer);
  const rows = [
    ["15.150", "Beton C30/37", "₺184.200"], ["15.160", "Çelik donatı Ø12", "₺96.450"], ["15.165", "Kalıp işçiliği", "₺41.300"],
    ["16.050", "Tuğla duvar", "₺28.900"], ["17.110", "İzolasyon, çatı", "₺33.120"], ["18.230", "Sıva, iç cephe", "₺19.780"],
    ["19.040", "Şap, zemin", "₺22.400"], ["20.120", "Seramik kaplama", "₺47.650"], ["21.300", "Alçıpan tavan", "₺18.250"],
    ["22.010", "Doğrama, PVC", "₺64.900"], ["23.115", "Boya, iç mekân", "₺15.330"], ["24.090", "Elektrik tesisatı", "₺58.400"],
    ["25.020", "Sıhhi tesisat", "₺39.870"], ["26.140", "Isıtma, kombi", "₺27.600"], ["27.005", "Merdiven korkuluğu", "₺12.480"],
    ["28.330", "Peyzaj", "₺9.950"], ["29.010", "İskele kirası", "₺14.200"], ["30.000", "Şantiye genel gideri", "₺21.000"],
  ];
  const tpl = $("#page-tpl");
  const pages = [0, 1, 2].map((i) => {
    const el = tpl.content.firstElementChild.cloneNode(true);
    el.querySelector(".pn").textContent = i + 1;
    const box = el.querySelector(".pg-rows");
    rows.slice(i * 6, i * 6 + 6).forEach((r) => {
      const row = document.createElement("div");
      row.className = "pg-row";
      row.innerHTML = `<span>${r[0]}</span><span>${r[1]}</span><span>${r[2]}</span>`;
      box.appendChild(row);
    });
    if (i === 2) {
      const seal = document.createElement("div");
      seal.className = "seal";
      seal.textContent = "APPROVED";
      el.appendChild(seal);
    }
    layer.appendChild(el);
    return {
      el, rows: $$(".pg-row", el), head: $(".pg-head", el), wm: $(".pg-wm", el),
      total: el.querySelectorAll(".pg-foot b")[1], seal: $(".seal", el),
      shown: -1, tot: -1,
    };
  });
  // fixed props of the Operations scene move into the same layer (one stacking order)
  const scan = $(".scan");
  const badgeMerge = $(".b-merge"), badgeSize = $(".b-size"), badgePdfa = $(".b-pdfa");
  const lock = $(".lock"), shackle = $(".shackle"), stamp = $(".stamp"), wipe = $(".wipe");
  [scan, badgeMerge, badgeSize, badgePdfa, lock, stamp, wipe].forEach((n) => layer.appendChild(n));

  let PW = 200, PH = 283, CX = 0, CY = 0, GAP = 220, codeBox = null;
  function layoutDesk() {
    // the layer may be display:none right now; show it for the measurement
    const prev = layer.style.display;
    layer.style.display = "block";
    PW = pages[0].el.offsetWidth;
    PH = pages[0].el.offsetHeight;
    layer.style.display = prev;
    if (mobile()) { CX = W / 2; CY = H * 0.71; GAP = PW * 1.08; }
    else { CX = W * 0.645; CY = H * 0.53; GAP = PW * 1.1; }
    codeBox = $(".code-card").getBoundingClientRect();
  }

  const setT = (el, x, y, s = 1, r = 0, sx = 1, sy = 1) => {
    el.style.transform = `translate3d(${x.toFixed(1)}px,${y.toFixed(1)}px,0) rotate(${r.toFixed(2)}deg) scale(${(s * sx).toFixed(4)},${(s * sy).toFixed(4)})`;
  };
  const showRows = (pg, n) => {
    if (pg.shown === n) return;
    pg.rows.forEach((r, i) => r.classList.toggle("on", i < n));
    pg.shown = n;
  };

  // ---------------------------------------------------------------- hero
  const heroMark = $(".hero-mark"), heroRot = $(".hm-rot"), brand = $(".brand"), brandMark = $(".brand-mark");
  const heroWords = $$(".hero-title .w"), heroSub = $(".hero-sub"), eyebrow = $(".eyebrow"), hint = $(".scroll-hint"), grid = $(".grid-bg");
  const heroStrokes = $$(".draw", heroMark);
  const heroP = $$(".s-p", heroMark), heroD = $$(".s-d", heroMark);
  heroStrokes.forEach((p) => { const L = p.getTotalLength(); p.style.strokeDasharray = L; p.style.strokeDashoffset = RM ? 0 : L; p._L = L; });
  const PAPER = [245, 243, 238], CORAL = [255, 90, 69];
  const mix = (a, b, t) => `rgb(${a.map((v, i) => Math.round(lerp(v, b[i], t))).join(",")})`;

  function hero(p, intro) {
    // intro: strokes draw in, words rise (time based, once)
    heroStrokes.forEach((path, i) => {
      const t = E.o3(seg(intro, 0.05 + i * 0.1, 0.55 + i * 0.1));
      path.style.strokeDashoffset = (path._L * (1 - t)).toFixed(1);
    });
    const m = RM ? 0 : E.io(seg(p, 0.02, 0.62));
    // travel into the nav logo: 180° turn, p and d trade colours so the turned mark lands identical
    // the hero stage is pinned at the top, so the mark's resting centre is fixed by its CSS
    const b = brandMark.getBoundingClientRect();
    const unitHero = Math.min(0.44 * H, 0.62 * W) / 512, unitBrand = b.height / 432; // svg has no offsetWidth; size from its CSS
    const s = lerp(1, unitBrand / unitHero, m);
    const dx = b.left + b.width / 2 - W / 2;
    const dy = b.top + b.height / 2 - H * 0.42;
    heroMark.style.transform = `translate(${(dx * m).toFixed(1)}px,${(dy * m).toFixed(1)}px) scale(${s.toFixed(4)})`;
    heroRot.style.transform = `rotate(${(180 * m).toFixed(2)}deg)`;
    heroRot.style.transformOrigin = "256px 256px";
    const swap = E.io(seg(m, 0.25, 0.75));
    heroP.forEach((n) => (n.style.stroke = mix(PAPER, CORAL, swap)));
    heroD.forEach((n) => (n.style.stroke = mix(CORAL, PAPER, swap)));
    const handoff = RM ? p > 0.35 : m > 0.985;
    heroMark.style.opacity = handoff ? 0 : RM ? 1 - seg(p, 0, 0.35) : 1;
    brand.classList.toggle("shown", handoff);

    heroWords.forEach((w, i) => {
      const into = E.o5(seg(intro, 0.42 + i * 0.07, 0.8 + i * 0.07));
      // a gentle lift and dim only: when the pin ends the headline rides away with the stage
      const out = E.io(seg(p, 0.5 + i * 0.05, 1));
      w.style.opacity = (into * (1 - 0.45 * out)).toFixed(3);
      w.style.transform = `translateY(${((1 - into) * 60 - out * 50).toFixed(1)}px)`;
    });
    const fade = 1 - 0.6 * seg(p, 0.4, 1);
    heroSub.style.opacity = (E.o3(seg(intro, 0.7, 1)) * fade).toFixed(3);
    eyebrow.style.opacity = (E.o3(seg(intro, 0.6, 0.95)) * fade).toFixed(3);
    hint.style.opacity = (seg(intro, 0.9, 1) * (1 - seg(p, 0, 0.06))).toFixed(3);
    grid.style.transform = `translateY(${(-p * 90).toFixed(1)}px) scale(${(1 + p * 0.12).toFixed(3)})`;
  }

  // ---------------------------------------------------------------- render
  const codeCard = $(".code-card"), codeLines = $$(".code-card .ln"), facts = $$(".facts li");
  const renderCaption = $(".render .caption");

  // fan positions shared with the ops scene
  const fan = () => [
    { x: CX - GAP, y: CY + PH * 0.02, r: -4 },
    { x: CX, y: CY, r: 0 },
    { x: CX + GAP, y: CY + PH * 0.02, r: 4 },
  ];

  function render(p, pe) {
    codeLines.forEach((l, i) => l.classList.toggle("on", p > 0.015 + i * 0.022));
    facts.forEach((f) => f.classList.toggle("on", p >= +f.dataset.at));
    const capIn = E.o3(seg(pe, 0.02, 0.16));
    renderCaption.style.opacity = capIn;
    renderCaption.style.transform = `translateY(${((1 - capIn) * 40).toFixed(1)}px)`;
    const cc = E.o3(seg(pe, 0.05, 0.22));
    codeCard.style.opacity = cc;
    codeCard.style.transform = `translateY(${((1 - cc) * 60).toFixed(1)}px) rotate(${lerp(-2, 0, cc).toFixed(2)}deg)`;

    const F = fan();
    const cx0 = codeBox.left + codeBox.width * 0.62, cy0 = codeBox.top + codeBox.height * 0.4;
    // A: printed out of the code card, travels to the desk, then slides left when B appears
    const a = E.io(seg(p, 0.16, 0.32));
    const aLeft = E.io(seg(p, 0.5, 0.6));
    const A = pages[0], B = pages[1], C = pages[2];
    setT(A.el, lerp(lerp(cx0, CX, a), F[0].x, aLeft), lerp(lerp(cy0, CY, a), F[0].y, aLeft), lerp(0.42, 1, a), lerp(lerp(-10, 0, a), F[0].r, aLeft));
    A.el.style.opacity = E.o3(seg(p, 0.16, 0.24));
    // B: comes out from under A, takes the centre
    const b = E.io(seg(p, 0.52, 0.6));
    setT(B.el, lerp(CX - GAP * 0.2, F[1].x, b), lerp(CY + 8, F[1].y, b), lerp(0.96, 1, b), 0);
    B.el.style.opacity = seg(p, 0.52, 0.56);
    // C: comes out from under B to the right
    const c = E.io(seg(p, 0.76, 0.84));
    setT(C.el, lerp(CX, F[2].x, c), lerp(CY + 8, F[2].y, c), lerp(0.96, 1, c), lerp(0, F[2].r, c));
    C.el.style.opacity = seg(p, 0.76, 0.8);

    showRows(A, Math.floor(clamp((p - 0.3) / 0.033, 0, 6)));
    showRows(B, Math.floor(clamp((p - 0.58) / 0.03, 0, 6)));
    showRows(C, Math.floor(clamp((p - 0.83) / 0.022, 0, 6)));
    // the repeated table header flashes on each new page; the page count grows as pages appear
    B.head.style.setProperty("--hl", bump(seg(p, 0.55, 0.68)).toFixed(3));
    C.head.style.setProperty("--hl", bump(seg(p, 0.79, 0.92)).toFixed(3));
    const tot = 1 + (p > 0.54) + (p > 0.78);
    pages.forEach((pg) => { if (pg.tot !== tot) { pg.total.textContent = tot; pg.tot = tot; } });

    // print head sweeping down the page being filled
    const sweep = (from, to, x, yc, r) => {
      const t = seg(p, from, to);
      return t > 0 && t < 1 ? { o: bump(t) * 1, x, y: yc - PH * 0.36 + PH * 0.74 * t, r } : null;
    };
    const sw = sweep(0.3, 0.5, CX, CY, 0) || sweep(0.58, 0.76, F[1].x, F[1].y, 0) || sweep(0.83, 0.96, F[2].x, F[2].y, F[2].r);
    if (sw) { scan.style.opacity = sw.o.toFixed(3); scan.style.transform = `translate3d(${sw.x}px,${sw.y}px,0) rotate(${sw.r}deg)`; }
    else scan.style.opacity = 0;
  }

  // ---------------------------------------------------------------- operations
  const opItems = $$(".oplist li"), opCmd = $(".opcmd"), opsCaption = $(".ops .caption"), opList = $(".oplist");
  let opActive = -1;
  const STEPS = 7, OPEND = 0.86, D = OPEND / STEPS, S = (k) => k * D;

  function ops(p, pe) {
    const capIn = E.o3(seg(pe, 0.12, 0.3));
    opsCaption.style.opacity = capIn;
    opsCaption.style.transform = `translateY(${((1 - capIn) * 40).toFixed(1)}px)`;
    const k = Math.min(STEPS - 1, Math.floor(p / D));
    if (k !== opActive) {
      opActive = k;
      opItems.forEach((li, i) => li.classList.toggle("on", i === k));
      opList.style.setProperty("--bar", opItems[k].offsetTop + "px");
      opCmd.textContent = opItems[k].querySelector("em").textContent.replace(/^pidief/, "pidief");
    }

    const F = fan();
    const stack = E.io(seg(p, S(0) + 0.1 * D, S(0) + 0.6 * D)) * (1 - E.io(seg(p, S(1) + 0.1 * D, S(1) + 0.7 * D)));
    const stackPos = [{ x: CX - 10, y: CY - 8, r: -3 }, { x: CX, y: CY, r: 0 }, { x: CX + 10, y: CY + 8, r: 3 }];
    const rot = E.io(seg(p, S(2) + 0.08 * D, S(2) + 0.5 * D)) * (1 - E.io(seg(p, S(2) + 0.72 * D, S(3))));
    const wm = E.io(seg(p, S(3) + 0.1 * D, S(3) + 0.6 * D));
    const comp = E.io(seg(p, S(4) + 0.05 * D, S(4) + 0.45 * D)) * (1 - 0.65 * E.io(seg(p, S(4) + 0.75 * D, S(5))));
    const squash = bump(seg(p, S(4) + 0.03 * D, S(4) + 0.35 * D)) * 0.06;

    pages.forEach((pg, i) => {
      let x = lerp(F[i].x, stackPos[i].x, stack), yy = lerp(F[i].y, stackPos[i].y, stack), r = lerp(F[i].r, stackPos[i].r, stack);
      let s = 1 - 0.13 * comp;
      if (i === 1) { r += 90 * rot; s *= lerp(1, 1 / 1.414, rot); }
      if (i === 0) x -= GAP * 0.13 * comp; if (i === 2) x += GAP * 0.13 * comp;
      setT(pg.el, x, yy, s, r, 1 + squash, 1 - squash);
      pg.el.style.zIndex = i === 1 ? 3 : i === 2 ? 2 : 1;
      pg.wm.style.opacity = (wm * 0.2).toFixed(3);
      pg.wm.style.letterSpacing = `${lerp(0.6, 0.18, wm).toFixed(3)}em`;
    });

    // badges
    const top = CY - PH * 0.5 - (mobile() ? 26 : 40);
    const bm = seg(p, S(0) + 0.4 * D, S(0) + 0.6 * D) * (1 - seg(p, S(1) + 0.05 * D, S(1) + 0.3 * D));
    badgeMerge.style.opacity = bm;
    badgeMerge.style.transform = `translate3d(${CX}px,${top - (1 - bm) * 14}px,0) scale(${lerp(0.8, 1, E.back(bm)).toFixed(3)})`;
    const bs = seg(p, S(4), S(4) + 0.2 * D) * (1 - seg(p, S(5) + 0.1 * D, S(5) + 0.3 * D));
    const mb = lerp(2.4, 0.38, E.io(seg(p, S(4) + 0.05 * D, S(4) + 0.55 * D)));
    badgeSize.querySelector(".to").textContent = mb.toFixed(2) + " MB";
    badgeSize.style.opacity = bs;
    badgeSize.style.transform = `translate3d(${CX}px,${top - (1 - bs) * 14}px,0)`;
    const bp = E.o3(seg(p, S(5), S(5) + 0.25 * D)) * (1 - seg(p, S(6), S(6) + 0.2 * D));
    badgePdfa.style.opacity = bp;
    badgePdfa.style.transform = `translate3d(${CX - GAP * 0.62}px,${top - (1 - bp) * 14}px,0)`;
    const lk = seg(p, S(5) + 0.1 * D, S(5) + 0.4 * D);
    const lockOut = seg(p, S(6), S(6) + 0.2 * D);
    lock.style.opacity = (Math.min(1, lk * 3) * (1 - lockOut)).toFixed(3);
    lock.style.transform = `translate3d(${CX + GAP * 0.62}px,${top - 12 + (1 - E.back(lk)) * -60}px,0)`;
    shackle.style.transform = `translateY(${(-12 * (1 - E.io(seg(p, S(5) + 0.45 * D, S(5) + 0.7 * D)))).toFixed(1)}px)`;

    // stamp: falls, squashes on impact, lifts and leaves the seal on page C
    const C = pages[2];
    const sealX = F[2].x + PW * 0.21, sealY = F[2].y + PH * 0.2;
    const fall = seg(p, S(6) + 0.12 * D, S(6) + 0.55 * D);
    const lift = E.o3(seg(p, S(6) + 0.68 * D, S(6) + 0.98 * D));
    const impact = bump(seg(p, S(6) + 0.52 * D, S(6) + 0.72 * D));
    const fy = lerp(-H * 0.3, sealY, E.i2(fall)) - lift * PH * 0.45;
    stamp.style.opacity = (Math.min(1, fall * 4) * (1 - seg(p, S(6) + 0.82 * D, S(6) + D))).toFixed(3);
    stamp.style.transform = `translate3d(${lerp(sealX + 140, sealX, E.o3(fall))}px,${fy}px,0) rotate(${lerp(-38, -12, E.o3(fall))}deg) scale(${(lerp(1.9, 1, E.i2(fall)) * (1 + impact * 0.1)).toFixed(3)},${(lerp(1.9, 1, E.i2(fall)) * (1 - impact * 0.12)).toFixed(3)})`;
    C.seal.style.opacity = seg(p, S(6) + 0.55 * D, S(6) + 0.6 * D);
    C.seal.style.transform = `scale(${lerp(1.18, 1, seg(p, S(6) + 0.55 * D, S(6) + 0.75 * D))})`;

    // the seal opens into the next scene
    const w = E.io(seg(p, OPEND + 0.02, 0.995));
    if (w > 0) {
      const R = Math.hypot(Math.max(sealX, W - sealX), Math.max(sealY, H - sealY)) * 1.04;
      wipe.style.display = "block";
      wipe.style.clipPath = RM ? "none" : `circle(${(R * w).toFixed(1)}px at ${sealX.toFixed(1)}px ${sealY.toFixed(1)}px)`;
      wipe.style.opacity = RM ? w : 1;
    } else wipe.style.display = "none";
    return w;
  }

  // ---------------------------------------------------------------- two doors
  const doorsHead = $$(".doors-head > *"), termLine = $(".term-line"), httpLine = $(".http-line"), httpRes = $(".http-res");
  const regRows = $$(".reg-row"), wires = $(".wires"), wireL = $(".w-l"), wireR = $(".w-r");
  const termBox = $(".door.term"), httpBox = $(".door.http"), gridBox = $(".doors-grid");
  const pairs = [
    ["pages rotate", "pidief pages rotate scan.pdf --degrees 90", "POST /v1/pages/rotate?degrees=90\nContent-Type: application/pdf"],
    ["html", "pidief html report.html --page-numbers", "POST /v1/html\nfiles=@report.html  pageNumbers=true"],
    ["merge", "pidief merge a.pdf b.pdf c.pdf", "POST /v1/merge\nfiles=@a.pdf  files=@b.pdf  files=@c.pdf"],
    ["render", "pidief render report.pdf --format webp --max-width 300", "POST /v1/render?format=webp&maxWidth=300"],
  ];
  let cycle = -1, wireLen = [1, 1];

  function wirePaths(rowName) {
    const row = regRows.find((r) => r.textContent === rowName);
    const g = gridBox.getBoundingClientRect(), t = termBox.getBoundingClientRect(), h = httpBox.getBoundingClientRect(), r = row.getBoundingClientRect();
    const ax = t.right - g.left, ay = t.top + t.height / 2 - g.top, bx = r.left - g.left, by = r.top + r.height / 2 - g.top;
    const cx = h.left - g.left, cy = h.top + h.height / 2 - g.top, dx = r.right - g.left;
    wires.setAttribute("viewBox", `0 0 ${g.width} ${g.height}`);
    wireL.setAttribute("d", `M${ax} ${ay} C${(ax + bx) / 2} ${ay} ${(ax + bx) / 2} ${by} ${bx - 4} ${by}`);
    wireR.setAttribute("d", `M${cx} ${cy} C${(cx + dx) / 2} ${cy} ${(cx + dx) / 2} ${by} ${dx + 4} ${by}`);
    wireLen = [wireL.getTotalLength(), wireR.getTotalLength()];
    [wireL, wireR].forEach((w, i) => (w.style.strokeDasharray = wireLen[i]));
  }

  function doors(p, pe) {
    doorsHead.forEach((n, i) => {
      const t = E.o3(seg(pe, 0.04 + i * 0.03, 0.2 + i * 0.03));
      n.style.opacity = t;
      n.style.transform = `translateY(${((1 - t) * 36).toFixed(1)}px)`;
    });
    const C0 = 0.08, CL = (0.98 - C0) / pairs.length;
    const k = clamp(Math.floor((p - C0) / CL), 0, pairs.length - 1);
    const t = p < C0 ? 0 : clamp((p - (C0 + k * CL)) / CL);
    if (k !== cycle) {
      cycle = k;
      regRows.forEach((r) => r.classList.toggle("on", false));
      if (!mobile()) wirePaths(pairs[k][0]);
    }
    const [name, cmd, req] = pairs[k];
    termLine.textContent = cmd.slice(0, Math.round(cmd.length * E.o3(seg(t, 0.02, 0.4))));
    httpLine.textContent = req.slice(0, Math.round(req.length * E.o3(seg(t, 0.18, 0.55))));
    const hit = t > 0.5 && t < 0.97;
    regRows.forEach((r) => r.classList.toggle("on", hit && r.textContent === name));
    httpRes.classList.toggle("on", t > 0.66 && t < 0.97);
    const wd = E.io(seg(t, 0.42, 0.66)) * (1 - seg(t, 0.9, 0.98));
    wireL.style.strokeDashoffset = (wireLen[0] * (1 - wd)).toFixed(1);
    wireR.style.strokeDashoffset = (wireLen[1] * (1 - wd)).toFixed(1);
  }

  // ---------------------------------------------------------------- install
  const endMark = $(".end-mark"), endStrokes = $$(".draw", endMark), installCopy = $$(".install-copy > *");
  endStrokes.forEach((p) => { const L = p.getTotalLength(); p.style.strokeDasharray = L; p._L = L; });
  function install(pe) {
    endStrokes.forEach((path, i) => {
      const t = E.o3(seg(pe, 0.2 + i * 0.07, 0.55 + i * 0.07));
      path.style.strokeDashoffset = (path._L * (1 - t)).toFixed(1);
    });
    // the closing turn mirrors the opening one
    const r = RM ? 0 : lerp(-180, 0, E.io(seg(pe, 0.2, 0.75)));
    endMark.style.transform = `rotate(${r.toFixed(2)}deg)`;
    installCopy.forEach((n, i) => {
      const t = E.o3(seg(pe, 0.32 + i * 0.07, 0.6 + i * 0.07));
      n.style.opacity = t;
      n.style.transform = `translateY(${((1 - t) * 40).toFixed(1)}px)`;
    });
  }

  // ---------------------------------------------------------------- frame
  const nav = $(".nav"), bar = $(".progress span"), numbersEl = $("#numbers");
  function frame(now) {
    const dt = Math.min(0.12, (now - lastFrame) / 1000);
    lastFrame = now;
    y = scrollY;
    ys = RM ? y : ys + (y - ys) * (1 - Math.exp(-dt * 9));
    if (Math.abs(y - ys) < 0.05) ys = y;
    const intro = RM ? 1 : clamp((now - t0) / 1900);
    const key = ys.toFixed(1) + "|" + (intro < 1 ? intro.toFixed(3) : "1") + "|" + W + "x" + H;
    if (key !== lastKey) {
      lastKey = key;
      const sh = scenes.hero, sr = scenes.render, so = scenes.ops, sd = scenes.doors, si = scenes.install;
      hero(prog(sh), intro);
      // the page layer lives from the Render entrance until the Doors scene has taken the screen
      const live = ys > sr.top - H * 0.6 && ys < sd.top + 2;
      if (live !== layer._live) { layer.style.display = live ? "block" : "none"; layer._live = live; }
      let w = 0;
      if (live) {
        // Render owns the pages until Operations pins; both scenes rest on the same fan in between.
        if (ys < so.top) { ops(0, enter(so)); render(prog(sr), enter(sr)); }
        else { render(1, 1); w = ops(prog(so), enter(so)); }
      }
      doors(prog(sd), enter(sd));
      install(enter(si));
      bar.style.transform = `scaleX(${clamp(ys / (document.documentElement.scrollHeight - H)).toFixed(4)})`;
      const dr = sd.el.getBoundingClientRect(), nr = numbersEl.getBoundingClientRect();
      nav.classList.toggle("on-coral", (dr.top < 40 && dr.bottom > 40) || w > 0.55);
      nav.classList.toggle("on-paper", nr.top < 40 && nr.bottom > 40);
    }
    requestAnimationFrame(frame);
  }

  // ---------------------------------------------------------------- numbers, copy buttons
  const io = new IntersectionObserver((entries) => {
    for (const e of entries) {
      if (!e.isIntersecting) continue;
      e.target.classList.add("in");
      io.unobserve(e.target);
      if (e.target.classList.contains("stat")) {
        const b = e.target.querySelector("b"), to = +b.dataset.count, dec = +(b.dataset.dec || 0), start = performance.now();
        const tick = (n) => {
          const t = RM ? 1 : E.o5(clamp((n - start) / 1500));
          b.textContent = (to * t).toFixed(dec);
          if (t < 1) requestAnimationFrame(tick);
        };
        requestAnimationFrame(tick);
      }
    }
  }, { threshold: 0.25 });
  $$(".reveal").forEach((el) => io.observe(el));

  $$(".copy").forEach((btn) => btn.addEventListener("click", async () => {
    const text = btn.parentElement.querySelector("code").textContent;
    try { await navigator.clipboard.writeText(text); }
    catch { const ta = document.createElement("textarea"); ta.value = text; document.body.appendChild(ta); ta.select(); document.execCommand("copy"); ta.remove(); }
    btn.textContent = "Copied"; btn.classList.add("done");
    setTimeout(() => { btn.textContent = "Copy"; btn.classList.remove("done"); }, 1400);
  }));

  // ---------------------------------------------------------------- layout
  function relayout(force) {
    W = innerWidth; H = innerHeight;
    // mobile toolbars change innerHeight while scrolling; only resize scenes on real changes
    if (force || W !== lastW || Math.abs(H - (relayout.h || H)) > 140) {
      sizeScenes(); relayout.h = H; lastW = W;
    }
    measure(); layoutDesk();
    cycle = -1; opActive = -1; lastKey = "";
  }
  addEventListener("resize", () => relayout(false));
  addEventListener("load", () => relayout(true));
  document.fonts && document.fonts.ready.then(() => relayout(true));
  relayout(true);
  requestAnimationFrame(frame);
})();
