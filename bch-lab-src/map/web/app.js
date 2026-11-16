"use strict";
(() => {
  // This window answers to a name, and links to Bitcoin Lab aim at Bitcoin Lab's name: clicking back and forth between
  // the two apps then reuses one window each instead of opening a new one per click. That needs the opener kept, so
  // these links carry no rel="noopener" - both windows are this Umbrel's own apps.
  window.name = "peer-map";
  // On a phone or tablet Safari loads a named tab in the background and stays where it is, so a tap seemed to do
  // nothing. There the jump stays in the same tab, and Back returns.
  const LAB_TARGET = "_self"; // one app, one tab: the map is a page of Bitcoin Cash Lab
  const GROUPS = ["inbound", "outbound", "manual"];
  const INBOUND_LIMIT = 10;
  // A peer reaching the node through Tor shows the exit's place and operator; the peer itself stays hidden behind it.
  const TOR_EXIT_TITLE = "Tor exit: location and operator are the exit's, not the peer's";
  // Same directions and symbols as Bitcoin Lab; Core reports both independently.
  const HB_TITLE = "One of your node's three high-bandwidth peers: it may send a new block without being asked first, which gives it a head start of at least one ping.";
  const HB_FROM_TITLE = "This peer has picked your node as one of its three high-bandwidth peers: your node sends it new blocks without being asked first.";
  const LABEL = { manual: "Manual", inbound: "Inbound", outbound: "Outbound" };
  const NS = "http://www.w3.org/2000/svg";
  const REGION_ZOOM = 2.5;                 // from this zoom factor on, markers split into regions
  const IDLE_PAUSE_MS = 30 * 60 * 1000;    // stop asking Core after 30 min without input
  const SIDE_KEY = "peermap.sideOpen";
  const $ = (id) => document.getElementById(id);

  const regionName = (() => {
    try {
      const dn = new Intl.DisplayNames(["en"], { type: "region" });
      return (cc) => { try { return dn.of(cc) || cc; } catch { return cc; } };
    } catch { return (cc) => cc; }
  })();

  // A peer that was not there at the last refresh lights up once, so what
  // changed since the last look is seen without searching. Not on the first
  // load: then everything would be new.
  const arrivedAt = new Map();
  let knownKeys = null;
  function markArrivals(peers) {
    const keys = new Set(peers.map((p) => p.group + " " + p.addr));
    const now = Date.now();
    if (knownKeys) for (const k of keys) if (!knownKeys.has(k)) arrivedAt.set(k, now);
    for (const [k, t] of arrivedAt) if (now - t > 3000 || !keys.has(k)) arrivedAt.delete(k);
    knownKeys = keys;
  }

  // Smooth scrolling, unless the reader asked the system for less motion.
  const glide = () => (window.matchMedia("(prefers-reduced-motion: reduce)").matches ? "auto" : "smooth");

  // Folding in place (10.11.2026, the same in Bitcoin Lab): what was tapped
  // stays where it is on the screen. What opens rolls out downwards and what
  // closes rolls up, gently, and the page never jumps - not to bring
  // something into view, not because the page got shorter at its end. Every
  // fold goes through here: a card, a group, a peer's panel, "Show all", a
  // legend.
  //
  //   anchor  what stays put, or a function finding it again (the tables
  //           are rebuilt, and with them the button that was tapped)
  //   box     what grows or shrinks, the same
  //   set     puts the page in the open (true) or closed (false) state
  const FOLD_MS = 300;
  let folding = 0;
  // Room below the page's end while something shrinks there; it goes as soon
  // as the reader scrolls up far enough not to need it.
  let runway = 0;
  function setRunway(px) {
    runway = Math.max(0, Math.ceil(px));
    document.body.style.paddingBottom = runway ? `${runway}px` : "";
  }
  window.addEventListener("scroll", () => {
    if (!runway || folding) return;
    const spare = document.documentElement.scrollHeight - (window.scrollY + window.innerHeight);
    if (spare > 0) setRunway(runway - spare);
  }, { passive: true });
  function foldInPlace({ anchor, box, set, open }) {
    const find = (x) => (typeof x === "function" ? x() : x);
    const still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    const a0 = find(anchor);
    if (!a0) { set(open); return; }
    const top0 = a0.getBoundingClientRect().top;
    // To where, not by how much: Safari reports a scroll a frame late, and
    // moving by the difference moved twice and swung back and forth.
    const pin = () => {
      const a = find(anchor);
      if (!a || !a.isConnected) return;
      const want = a.getBoundingClientRect().top + window.scrollY - top0;
      if (Math.abs(window.scrollY - want) >= 0.5) window.scrollTo(0, want);
    };
    folding += 1;
    // Room at the end for as much as the box could lose, lent before anything
    // is measured: measuring a page that got shorter is already enough for
    // the browser to pull everything down. When the fold is over, only what
    // the reader still needs stays.
    const lent = runway;
    const b0 = find(box);
    const h0 = b0 ? b0.getBoundingClientRect().height : 0;
    setRunway(lent + h0);
    const giveBack = () => {
      const end = document.documentElement.scrollHeight - runway;
      setRunway(Math.max(lent, window.scrollY + window.innerHeight - end));
    };
    const done = () => { pin(); giveBack(); pin(); requestAnimationFrame(() => { pin(); folding -= 1; freed(); }); };
    // A fold that fails half way must not leave refreshes held back.
    try { set(open); } catch (err) { folding -= 1; throw err; }
    const b1 = find(box);
    const h1 = b1 ? b1.getBoundingClientRect().height : 0;
    // A whole list of peers is tens of thousands of pixels: rolled in a third
    // of a second that rushes past at a screen per frame. That opens and
    // shuts in place at once, the rows fading in.
    const huge = Math.abs(h1 - h0) > window.innerHeight * 1.5;
    if (huge && open && !still && b1 && b1.animate) b1.animate([{ opacity: 0.25 }, { opacity: 1 }], { duration: 220, easing: "ease-out" });
    if (still || huge || !b1 || !b1.animate || Math.abs(h1 - h0) < 2) { pin(); done(); return; }
    // Closing rolls up what is still there: back to open for the animation,
    // closed at its end.
    if (!open) set(true);
    const b = find(box);
    const anim = b.animate([{ height: `${h0}px`, overflow: "hidden" }, { height: `${h1}px`, overflow: "hidden" }],
      { duration: FOLD_MS, easing: open ? "cubic-bezier(.2,.7,.3,1)" : "cubic-bezier(.5,0,.3,1)" });
    let frame = requestAnimationFrame(function follow() { pin(); frame = requestAnimationFrame(follow); });
    anim.onfinish = anim.oncancel = () => {
      cancelAnimationFrame(frame);
      try { if (!open) set(false); } finally { done(); }
    };
  }

  const state = {
    data: null,           // last /api/peers reply
    world: null,          // world.json
    show: { manual: true, inbound: true, outbound: true },
    place: null,          // location filter from clicking a marker: { key, label }
    group: null,          // first level of the kind filter: Nodes, Wallets, Watchers, Disguised, Other
    kind: null,           // kind filter from clicking an entry in a group's kind line
    sub: null,            // one level under the kind: a pool, a version, a name
    label: null,          // beside it, for nodes: the operator's own label, "(FutureBit-Apollo-Node)"
    allSubs: false,       // every chip one level down, not one line of them
    openRows: new Set(),  // rows the reader has opened by a tap or click: "group address"
    sort: {},             // per group: { key, dir }
    view: { k: 1, x: 0, y: 0 },
    paused: false,
    // A peer another app pointed at, via ?peer=<address>. Highlighted once and
    // scrolled to, then left alone - it is a starting point, not a filter.
    focus: new URLSearchParams(location.search).get("peer") || null,
    focusDone: false,
    lastHash: null,       // newest block hash seen, to pulse the map when it changes
    lastInput: Date.now(),
    // When the current snapshot arrived here. The block's age comes from the
    // node's clock; how long it has been sitting in this browser is added on
    // top, so the two-minute mark never depends on the browser's own clock
    // agreeing with the node's.
    receivedAt: 0,
    liveShown: false,
    showAllInbound: false, // the inbound table shows its top INBOUND_LIMIT until asked for all
  };

  const store = {
    get(k) { try { return localStorage.getItem(k); } catch { return null; } },
    set(k, v) { try { localStorage.setItem(k, v); } catch { /* private mode etc. */ } },
  };

  // Every card folds to its title on a tap, and stays folded on this device:
  // open unless the reader closed it (28.10.2026).
  const FOLD_KEY = "peermap.folded";
  const folded = new Set((() => { try { return JSON.parse(store.get(FOLD_KEY)) || []; } catch { return []; } })());
  function foldable(card, title, id) {
    title.classList.add("fold-title");
    title.tabIndex = 0;
    title.setAttribute("role", "button");
    const show = () => {
      card.classList.toggle("folded", folded.has(id));
      title.setAttribute("aria-expanded", String(!folded.has(id)));
    };
    const flip = (e) => {
      if (e.target.closest("button, a")) return;
      // What is on screen: a card opened for a peer link is open, whatever
      // the stored state says.
      const opening = card.classList.contains("folded");
      foldInPlace({ anchor: title, box: card, open: opening, set: (open) => {
        if (open) folded.delete(id); else folded.add(id);
        show();
        // Folded, a chip row measured no width and was left whole: fit it
        // now, and the tables with it, whose lines were measured folded too.
        if (open) {
          for (const row of card.querySelectorAll(".one-line")) fitOneLine(row);
          fitTables();
        }
      } });
      // Kept as the reader left it, not as it is half way: closing rolls up
      // from open and is folded only at the end.
      if (opening) folded.delete(id); else folded.add(id);
      store.set(FOLD_KEY, JSON.stringify([...folded]));
      if (id === "map" && opening) window.dispatchEvent(new Event("resize"));
    };
    title.addEventListener("click", flip);
    title.addEventListener("keydown", (e) => { if (e.key === "Enter" || e.key === " ") { e.preventDefault(); flip(e); } });
    show();
  }
  foldable($("mapCard"), $("mapHead"), "map");
  // The block card folds to its summary line, but only when a route is drawn
  // under it (has-route): without one it is a single line already.
  foldable($("blockCard"), $("blockSummary"), "block");

  // ---------- projection: same Natural Earth polynomial as tools/genmap ----------
  function nePoly(lon, lat) {
    const l = lon * Math.PI / 180, p = lat * Math.PI / 180;
    const p2 = p * p, p4 = p2 * p2;
    return [
      l * (0.8707 - 0.131979 * p2 + p4 * (-0.013791 + p4 * (0.003971 * p2 - 0.001529 * p4))),
      p * (1.007226 + p2 * (0.015085 + p4 * (-0.044475 + 0.028874 * p2 - 0.005916 * p4))),
    ];
  }
  const X_MAX = nePoly(180, 0)[0];
  const Y_MAX = nePoly(0, 84)[1];
  function project(lon, lat) {
    const [x, y] = nePoly(lon, lat);
    const s = 1000 / (2 * X_MAX);
    return [(x + X_MAX) * s, (Y_MAX - y) * s];
  }

  // ---------- polling: only while visible, and not after 30 min idle ----------
  let pollTimer = 0;
  let tickTimer = 0;
  let inFlight = false;

  // Exactly one polling chain at a time. Without the inFlight guard, a tab
  // that became visible again while a request was still running started a
  // second chain next to the first, and every such moment added another.
  async function refresh() {
    clearTimeout(pollTimer);
    if (inFlight || document.hidden || state.paused) return;
    if (Date.now() - state.lastInput > IDLE_PAUSE_MS) {
      setPaused(true);
      return;
    }
    let next = 10;
    inFlight = true;
    try {
      const r = await fetch("api/peers", { cache: "no-store" });
      if (!r.ok) throw new Error("HTTP " + r.status);
      state.data = await r.json();
      state.receivedAt = Date.now();
      markArrivals(state.data.peers);
      next = state.data.next_in || state.data.interval || 10;
      state.failed = false;
      renderWhenFree();
      beat();
    } catch (e) {
      state.failed = true;
      renderStatus();
      showError("Peer Map backend not reachable: " + e.message);
    } finally {
      inFlight = false;
    }
    clearTimeout(pollTimer);
    if (!document.hidden && !state.paused) pollTimer = setTimeout(refresh, next * 1000 + 250);
  }

  function setPaused(p) {
    state.paused = p;
    $("paused").hidden = !p;
    if (p) clearTimeout(pollTimer);
    renderStatus();
  }

  let inputStamp = 0;
  const noteInput = () => {
    const now = Date.now();
    if (now - inputStamp < 1000) return; // cheap throttle
    inputStamp = now;
    state.lastInput = now;
  };
  for (const ev of ["pointerdown", "pointermove", "keydown", "wheel", "touchstart", "scroll"]) {
    window.addEventListener(ev, noteInput, { passive: true, capture: true });
  }
  $("resume").addEventListener("click", () => {
    state.lastInput = Date.now();
    setPaused(false);
    refresh();
  });

  document.addEventListener("visibilitychange", () => {
    if (document.hidden) {
      clearTimeout(pollTimer);
      clearInterval(tickTimer);
      tickTimer = 0;
    } else {
      startTicker();
      refresh();
    }
  });

  function startTicker() {
    if (!tickTimer) {
      tickTimer = setInterval(() => {
        renderStatus();
        // Two minutes are up between two polls, and a paused dashboard never
        // polls again at all - so the star and the row colour take themselves
        // off rather than waiting for the next answer.
        if (state.liveShown && !liveBlock()) renderWhenFree();
      }, 1000);
    }
  }

  function showError(msg) {
    const el = $("error");
    el.textContent = msg;
    el.hidden = !msg;
  }

  // ---------- formatting ----------
  function ago(sec) {
    if (sec < 60) return Math.max(0, Math.floor(sec)) + "s";
    const m = Math.floor(sec / 60), h = Math.floor(m / 60), d = Math.floor(h / 24);
    if (d > 0) return d + "d " + (h % 24) + "h";
    if (h > 0) return h + "h " + (m % 60) + "m";
    return m + "m";
  }
  const typeLabel = (t) => ({
    "outbound-full-relay": "Full relay",
    "block-relay-only": "Block relay",
  }[t] || t || "-");
  // As Bitcoin Lab writes it: milliseconds below a second, seconds above.
  const fmtPing = (ms) => (ms < 1000 ? Math.round(ms) + " ms" : (ms / 1000).toFixed(2) + " s");
  const netLabel = (n) => ({
    ipv4: "IPv4", ipv6: "IPv6", onion: "Tor", i2p: "I2P", cjdns: "CJDNS",
    not_publicly_routable: "Private", ipv6_hidden: "IPv6 (address hidden)",
    tor_hidden: "Tor (address hidden)", i2p_hidden: "I2P (address hidden)",
  }[n] || n || "-");
  const HIDDEN = {
    ipv6_hidden: { short: "IPv6", tip: "Inbound IPv6 passes through Docker, which puts its own address in place of the peer's." },
    tor_hidden: { short: "Tor", tip: "Reached through the Tor proxy, which hides the peer's address." },
    i2p_hidden: { short: "I2P", tip: "Reached through the I2P router, which hides the peer's address." },
  };
  // A flag from the two-letter country code: regional-indicator letters, no
  // image files. Unknown or missing codes get none.
  const HIDDEN_FLAG = {
    not_publicly_routable: "\u{1F3E0}",
    onion: "\u{1F9C5}", tor_hidden: "\u{1F9C5}",
    i2p: "\u{1F310}", i2p_hidden: "\u{1F310}", ipv6_hidden: "\u{1F310}",
  };
  const flagOf = (cc) => (/^[A-Z]{2}$/.test(cc || "") ? String.fromCodePoint(...[...cc].map((c) => 0x1f1e6 + c.charCodeAt(0) - 65)) : "");
  const locLabel = (p) => (p.cc ? (p.region ? `${p.region}, ${regionName(p.cc)}` : regionName(p.cc)) : "");

  // ---------- the block that just landed ----------

  // Bitcoin Lab next door says which block arrived last, who mined it, and
  // which peer here delivered it. The mark lasts two minutes - long enough to
  // see on a dashboard somebody just opened, short enough that a star stays a
  // piece of news instead of turning into decoration.
  function liveBlock() {
    const b = state.data && state.data.last_block;
    if (!b || !b.mark_for_ms) return null;
    const age = b.age_ms + Math.max(0, Date.now() - state.receivedAt);
    return age < b.mark_for_ms ? b : null;
  }

  // Refreshed once per render and read by the table and the map, so both
  // always show the same peers starred.
  let deliveredNow = new Set();
  let deliveredEver = new Set();

  function deliveredSet() {
    const b = liveBlock();
    return new Set(b && b.first_peers ? b.first_peers : []);
  }

  // The card under the filters: one block, and the three roles it has here.
  // Mined by (from Bitcoin Lab's coinbase reading), delivered by (the peers
  // Core credited), raced by (the pools that turned it into work). Each of the
  // three can be missing, and then it says so rather than leaving a gap.
  function renderBlockCard() {
    const card = $("blockCard");
    if (!card) return;
    const b = state.data && state.data.last_block;
    card.hidden = !b;
    if (!b) return;

    // With a route the card stays open: the summary and the route are the
    // point, and only the full pool list folds away. Without one - Stratum
    // Race off, or no race recorded for this block - it is today's card, a
    // single line that opens onto who delivered it.
    const own = routeStops(b);
    const typical = medianStops(b);
    const view = state.routeView === "median" && typical ? "median" : "block";
    // The card is open whenever there is any route to show - this block's,
    // or the typical one if this block happens to have no race.
    const stops = own || typical ? (view === "median" ? typical : own || typical) : null;
    const more = $("blockMore");
    const line = stops ? $("blockSummary") : $("blockMoreLabel");
    $("blockSummary").hidden = !stops;
    card.classList.toggle("has-route", Boolean(stops));
    $("blockRoute").hidden = !stops;
    more.classList.toggle("minor", Boolean(stops));
    fillSummary(line, b);
    if (stops) {
      // This block may have no race of its own while the typical route
      // exists - Stratum Race was off for a moment, say. Then the fold holds
      // what this block does have: who delivered it.
      const entries = (b.stratum && b.stratum.entries) || [];
      $("blockMoreLabel").textContent = entries.length
        ? `All ${entries.length} ${entries.length === 1 ? "pool" : "pools"}`
        : "Delivered first";
      renderRoute($("blockRoute"), b, stops, {
        view: stops === typical ? "median" : "block",
        canSwitch: Boolean(own && typical),
        noRaceHere: !own,
      });
    }
    // "Delivered first" is on the route only when this block's own route is.
    renderBlockBody(b, Boolean(own) && stops === own);
    // One switch for the whole card: where the route has one, it sets the
    // compact block line too; without a route the line brings its own.
    renderRecon($("blockRecon"), b, own && typical ? view : null);
  }

  // How Core put the block together from its compact form (BIP152), as
  // Bitcoin Lab read it from getpeerinfo: from the mempool alone, or after
  // asking a peer for the transactions it did not have - a full round trip -
  // or downloaded in full. This block, or the share over the last hundred
  // blocks - the same "typical" as the route's, and the same switch.
  // Absent with a Bitcoin Lab from before it measured this.
  const RECON_WHY = "A new block arrives as a compact block: short ids for its transactions, " +
    "which your node fills in from its mempool and from transactions it recently turned down. " +
    "Whatever it does not have, it asks the peer for - one more round trip before the block is complete.";
  // No-break spaces keep a number and its unit on one line on a phone.
  const kB = (bytes) => (bytes < 1000 ? "under 1\u00a0kB" : Math.round(bytes / 1000).toLocaleString("en-US") + "\u00a0kB");

  // What was missing as a share of the whole block, once Bitcoin Lab knows its size.
  function shareOfBlock(b) {
    if (!b.block_size || b.recon_missing_bytes == null) return "";
    const p = (100 * b.recon_missing_bytes) / b.block_size;
    return ", " + (p < 1 ? "under 1\u00a0%" : Math.round(p) + "\u00a0%") + " of the block";
  }

  function renderRecon(box, b, routeView) {
    const last = b.recon_last100;
    const has = b.recon || last;
    box.hidden = !has;
    box.textContent = "";
    if (!has) return;
    // The route's switch decides where there is one; otherwise the same
    // choice, remembered the same way, on a switch of the line's own.
    const view = (routeView || state.routeView) === "median" && last ? "median" : "block";

    const head = document.createElement("span");
    head.className = "recon-head";
    head.textContent = "Compact block";
    head.title = RECON_WHY;
    box.appendChild(head);
    if (!routeView && last) {
      const sw = document.createElement("div");
      sw.className = "rt-switch";
      for (const [v, label] of [["block", "This block"], ["median", "Last 100 blocks"]]) {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.textContent = label;
        btn.className = v === view ? "on" : "";
        btn.addEventListener("click", () => setRouteView(v));
        sw.appendChild(btn);
      }
      box.appendChild(sw);
    }
    const line = document.createElement("p");
    line.className = "recon-line";
    line.title = RECON_WHY;
    if (view === "block") {
      line.textContent = {
        mempool: "Rebuilt from what your node already had, nothing was missing",
        asked: `Your node had to ask for missing transactions: ${kB(b.recon_missing_bytes || 0)}` + shareOfBlock(b),
        full: "Downloaded in full, not rebuilt",
        unclear: "Not clear for this block",
      }[b.recon] || "Not measured for this block";
    } else {
      const w = last;
      const pct = (n) => Math.round((100 * n) / w.blocks) + "\u00a0%";
      const parts = [`${pct(w.mempool)} rebuilt from what your node had`];
      if (w.asked) parts.push(`${pct(w.asked)} needed missing transactions` + (w.asked_avg_bytes != null ? `, ${kB(w.asked_avg_bytes)} on average` : ""));
      if (w.full) parts.push(`${pct(w.full)} downloaded in full`);
      line.textContent = parts.join(" \u00b7 ") + " ";
      const n = document.createElement("small");
      n.textContent = `\u00b7 ${w.blocks.toLocaleString("en-US")} of the last 100 blocks measured`;
      if (w.blocks < 100) line.appendChild(n);
    }
    box.appendChild(line);
  }

  // The line is parts, not words: "mined by Foundry USA" moves to the next
  // line as one, and the dot in front of a part that starts a line is clipped
  // (see .b-parts), so no line begins with a stray "·" on a phone.
  function fillSummary(sum, b) {
    sum.textContent = "";
    const line = document.createElement("span");
    line.className = "b-line";
    const parts = document.createElement("span");
    parts.className = "b-parts";
    line.appendChild(parts);
    sum.appendChild(line);
    const part = () => {
      const el = document.createElement("span");
      el.className = "b-part";
      parts.appendChild(el);
      return el;
    };
    const add = (into, cls, text, title) => {
      const el = document.createElement("span");
      if (cls) el.className = cls;
      el.textContent = text;
      if (title) el.title = title;
      into.appendChild(el);
      return el;
    };

    add(part(), "b-height", b.height ? "Block " + b.height.toLocaleString("en-US") : "Last block", b.hash || "");
    // Who mined it, without "mined by": the orange name says it, and on a
    // phone the words were what pushed the line to wrap.
    if (b.pool || b.pool_tag) {
      const mined = part();
      // A miner's own text stays on one line and ends in … when it is too long.
      if (!b.pool) mined.classList.add("b-cut");
      // The full name, as on mempool.space: "Foundry USA", not "Foundry".
      const name = b.pool_name || b.pool;
      add(mined, "b-pool" + (name ? "" : " raw"), name || `"${b.pool_tag}"`,
        name ? `Mined by ${name}` : `Mined by a pool we do not know - this is the text in its coinbase: "${b.pool_tag}"`);
    } else if (b.pool_source === "none") {
      // Only once the Lab has read the coinbase and found nothing: before
      // that, or when it could not read it, nothing was checked yet.
      add(part(), "b-unknown", "Unknown", "Mined by a pool we do not know: neither the payout address nor the coinbase text matched");
    }
    const n = (b.first_peers || []).length;
    // One peer and a known place: say where it came from, with its flag.
    const one = n === 1 && state.data.peers.find((p) => p.addr === b.first_peers[0]);
    add(part(), null, n === 0 ? "nobody credited" : one && one.cc ? `delivered from ${flagOf(one.cc)} ${regionName(one.cc)}`
      : n === 1 ? "delivered by 1 peer" : `delivered by ${n} peers`,
      b.eligible ? `${b.eligible} peers were connected when it arrived` : "");
    add(part(), "miss", ago(blockAgeMs(b) / 1000) + " ago", "How long ago your node saw this block");
  }

  const blockAgeMs = (b) => b.age_ms + Math.max(0, Date.now() - state.receivedAt);

  // ---------- the route of one block ----------
  //
  // Where the time between "this block exists" and "your pool has a job for
  // it" goes, drawn to scale. The zero is the first new job any pool sent
  // here: when the block was found cannot be known - the header's time is the
  // miner's own, in whole seconds - and the first job is the earliest moment
  // it is visible from this node. After that everything is measured by one
  // clock on one machine, except the last hop into Core, which is estimated
  // as the delivering peer's ping.
  //
  // How many pings, as BIP152 counts them (hopPings): a high-bandwidth peer
  // sends the block unasked, half a ping; any other announces it and is
  // asked, a ping and a half; asking for missing transactions adds one.
  // Bitcoin Lab says which kind of peer it was just before the block. It
  // only moves the peer's dot - Core and the pools stand where they were
  // measured.
  const ROUTE_STUB = 140;  // the miner's dashed lead-in, not to scale
  // Not known whether the peer was high-bandwidth (older blocks): one ping,
  // the middle. Bitcoin Lab's typical route counts the same way.
  const hopPings = (hb, recon) => (hb === 1 ? 0.5 : hb === 0 ? 1.5 : 1) + (recon === "asked" ? 1 : 0);
  const HOP_WHY = "Estimated from the peer's ping, as BIP152 counts it: a high-bandwidth peer sends a new block " +
    "without being asked, half a ping; any other announces it first, a ping and a half. Asking for missing " +
    "transactions adds a ping. These are minimums - more missing data or a slow line take longer, and a peer " +
    "still busy with the block - checking it, sending it to others - answers only after that. Measured elsewhere (0xB10C, 2024), asking " +
    "added about 115 ms in the median.";
  // Text cut off with an ellipsis is readable on hover - and only then, a
  // tooltip that repeats what is on screen says nothing. Whether it is cut
  // depends on the screen, so it is decided when the pointer gets there.
  // `tip` is what the element says on hover anyway, cut or not.
  function fit(e, full, tip) {
    e.classList.add("fit");
    e.dataset.full = full;
    if (tip) e.dataset.tip = tip;
    return e;
  }
  document.addEventListener("mouseover", (ev) => {
    const e = ev.target.closest && ev.target.closest(".fit");
    if (!e) return;
    const cut = e.scrollWidth > e.clientWidth;
    const link = e.querySelector("a[data-tip]");
    const lines = [cut ? e.dataset.full : "", e.dataset.tip || ""];
    if (link) link.title = [...lines, link.dataset.tip].filter(Boolean).join("\n");
    // No title at all rather than an empty one: an empty title hides the
    // cell's own (the Tor exit explanation on the operator).
    else if (lines.some(Boolean)) e.title = lines.filter(Boolean).join("\n");
    else e.removeAttribute("title");
  });

  const ROUTE_GAP = 150;   // the closest two stops may sit, so labels never touch

  const hostOf = (a) => (a.startsWith("[") ? a.slice(1, a.indexOf("]")) : a.replace(/:\d+$/, ""));
  // A stop says no time counted from the first job: with the owner's own
  // pool first, every stop before it read "−206 ms" (04.11.2026). The line
  // between two stops says how long that stretch took; the first job is no
  // station, so it says where it stands against your node instead.
  const firstJobWords = (win, stops) => {
    const core = stops.find((st) => st.kind === "core");
    const d = core ? Math.round(core.t - win.t) : 0;
    return d === 0 ? "same moment as your node" : Math.abs(d) + " ms " + (d > 0 ? "before" : "after") + " your node";
  };

  // Which route the card shows: this block, or the typical one. Remembered
  // per browser, because somebody tuning a setup wants the median every time.
  const ROUTE_VIEW_KEY = "peermap.routeView";
  state.routeView = (() => { try { return localStorage.getItem(ROUTE_VIEW_KEY) || "block"; } catch { return "block"; } })();
  function setRouteView(v) {
    state.routeView = v;
    try { localStorage.setItem(ROUTE_VIEW_KEY, v); } catch { /* private window: fine */ }
    renderBlockCard();
  }

  function routeStops(b) {
    const s = b.stratum;
    if (!s || s.core_ms == null || !(s.entries || []).length) return null;
    const winner = s.entries.find((e) => e.rank === 1 && e.latency_ms != null);
    if (!winner) return null;
    const own = s.entries.find((e) => e.own && e.latency_ms != null) || null;

    const stops = [];
    // The winner is the zero. When it is the owner's own pool the two are one
    // stop, and it is drawn as his.
    if (own !== winner) stops.push({ kind: "win", t: 0, name: winner.label, sub: "first job" });

    const known = new Map(((state.data && state.data.peers) || []).map((p) => [p.addr, p]));
    const credited = (b.first_peers || []).map((addr) => ({ addr, peer: known.get(addr) }));
    // The ping Bitcoin Lab took from the snapshot that credited the peer is
    // the right one - it is from the moment the block arrived. Only a block
    // from before the Lab stored it falls back to what Core says now.
    const pingOf = (c) => (c.peer ? (c.peer.min_ping_ms != null ? c.peer.min_ping_ms : c.peer.ping_ms) : null);
    const lead = credited.filter((c) => pingOf(c) != null).sort((x, y) => pingOf(x) - pingOf(y))[0]
      || (credited.length && b.first_ping_ms != null ? credited[0] : null);
    const ping = b.first_ping_ms != null ? b.first_ping_ms : (lead ? pingOf(lead) : null);
    if (lead && ping != null) {
      const p = lead.peer;
      const where = p ? p.region || (p.cc ? regionName(p.cc) : "") : "";
      const more = credited.length > 1 ? ` +${credited.length - 1}` : "";
      stops.push({
        kind: "peer", group: p ? p.group : "", t: s.core_ms - ping * hopPings(b.first_hb, b.recon),
        pings: hopPings(b.first_hb, b.recon), asked: b.recon === "asked", hb: b.first_hb,
        name: hostOf(lead.addr) + more,
        sub: p ? [where, p.operator, p.tor_exit ? "Tor exit" : ""].filter(Boolean) : ["no longer connected"],
        title: credited.map((c) => c.addr).join("\n"),
      });
    }
    stops.push({
      kind: "core", t: s.core_ms, name: "Node",
      // Credited, but with no ping to place it by: named rather than drawn.
      sub: [!(lead && ping != null) && credited.length ? "via " + hostOf(credited[0].addr) : "your node"],
    });
    if (b.template_ms != null) {
      stops.push({ kind: "tpl", t: s.core_ms + b.template_ms, name: "Template", sub: ["ready in your node"], tx: b.template_tx });
    }
    if (own) stops.push({ kind: "own", t: own.latency_ms, name: own.label, sub: ["your job"] });
    for (const st of stops) if (typeof st.sub === "string") st.sub = [st.sub];
    return byTime(stops);
  }

  // Core's template time is kept in whole milliseconds, the pool's job to the
  // fraction, and with small templates the job is ready the same moment:
  // rounding alone can put the job before the template it was built from.
  // It cannot be, so the template waits for it, and the job stretch - the
  // one that says how many transactions the template carried - stays.
  function byTime(stops) {
    const tpl = stops.find((s) => s.kind === "tpl");
    const own = stops.find((s) => s.kind === "own");
    if (tpl && own && own.t < tpl.t) tpl.t = own.t;
    return stops.sort((x, y) => x.t - y.t);
  }

  // The typical block: every stop at its median over the last hundred, each
  // counted from its own first job. Stops are medians of their own, so they
  // are not one real block - they are where each stop usually is, which is
  // the thing that moves when something is changed.
  function medianStops(b) {
    const m = b.route_median;
    if (!m || !m.core) return null;
    const n = (st) => `median of ${st.n} ${st.n === 1 ? "block" : "blocks"}`;
    const stops = [{ kind: "win", t: 0, name: "First pool", sub: ["first job"] }];
    if (m.peer) stops.push({ kind: "peer", t: m.peer.ms, name: "Your peer", sub: ["has the block"], title: n(m.peer), plain: true });
    stops.push({ kind: "core", t: m.core.ms, name: "Node", sub: ["your node"], title: n(m.core) });
    if (m.template) stops.push({ kind: "tpl", t: m.template.ms, name: "Template", sub: ["ready in your node"], title: n(m.template), tx: m.tx && m.tx.ms });
    if (m.own) stops.push({ kind: "own", t: m.own.ms, name: m.own_label || "Your pool", sub: ["your job"], title: n(m.own) });
    return byTime(stops);
  }

  // To scale, but never so close that two labels touch: a stop that would
  // sit nearer than ROUTE_GAP to the one before is pushed along, and the
  // whole row pulled back in from the right. Too narrow for that at all - a
  // phone - and it is a list instead, which says the same thing.
  function placeStops(stops, left, right, need) {
    const gap = (i) => (need && need[i]) || ROUTE_GAP;
    const t0 = stops[0].t, span = Math.max(1, stops[stops.length - 1].t - t0);
    const x = stops.map((s) => left + ((s.t - t0) / span) * (right - left));
    for (let i = 1; i < x.length; i++) x[i] = Math.max(x[i], x[i - 1] + gap(i));
    x[x.length - 1] = Math.min(x[x.length - 1], right);
    for (let i = x.length - 2; i >= 0; i--) x[i] = Math.min(x[i], x[i + 1] - gap(i + 1));
    return x;
  }
  // How far apart two stops must be for the stretch label between them.
  let measureCtx = null;
  function segNeeds(box, stops) {
    measureCtx = measureCtx || document.createElement("canvas").getContext("2d");
    measureCtx.font = "12px " + getComputedStyle(box).fontFamily;
    const need = stops.map(() => ROUTE_GAP);
    const journey = stops.filter((st) => st.kind !== "win");
    const win = stops.find((st) => st.kind === "win");
    const pairs = [];
    if (win && journey.length && win.t <= journey[0].t) pairs.push([win, journey[0]]);
    for (let i = 1; i < journey.length; i++) pairs.push([journey[i - 1], journey[i]]);
    for (const [a, z] of pairs) {
      const w = measureCtx.measureText(segLabel(a, z)[0]).width + 24;
      const ia = stops.indexOf(a), iz = stops.indexOf(z);
      for (let k = ia + 1; k <= iz; k++) need[k] = Math.max(need[k], w / (iz - ia));
    }
    return need;
  }

  // One stretch, the same words on every width: what happens on it, how
  // long it took, and the longer why for the tooltip. The hop into Core says
  // why it is that long - sent unasked (⚡) or announced first, and whether
  // Core had to ask for missing transactions; "at least" then, because the
  // round trip is only the floor.
  function stretch(a, z) {
    const ms = Math.round(z.t - a.t).toLocaleString("en-US") + "\u00a0ms";
    if (a.kind === "peer" && z.kind === "core") {
      const kind = a.hb === 1 ? "⚡" : a.hb === 0 ? "announced" : "";
      const what = kind && a.asked ? kind + " + asked" : kind || (a.asked ? "asked" : "ping");
      return [what, (a.asked ? "at least\u00a0" : "~") + ms, HOP_WHY];
    }
    if (a.kind === "win" && z.kind === "peer") return ["reaches your peer", ms, ""];
    if (a.kind === "core" && z.kind === "tpl") return ["template", ms, "Your node builds a new block template (getblocktemplate)"];
    if (a.kind === "tpl" && z.kind === "own") {
      // The template's size is most of the reason this stretch varies: the
      // pool handles every transaction in it before it can send the job.
      const tx = a.tx != null ? " · " + Math.round(a.tx).toLocaleString("en-US") + " tx" : "";
      return ["job" + tx, ms, "Your pool notices the block and turns the template into a job" +
        (tx ? " - the more transactions the template carries, the longer that takes" : "")];
    }
    // No template timing for this block: the two are one stretch.
    if (a.kind === "core" && z.kind === "own") {
      return ["template + job", ms, "Your node builds a new block template (getblocktemplate), your pool notices the block and turns it into a job"];
    }
    return ["", ms, ""];
  }
  // The wide route's label: first what, then how long - as on a phone.
  function segLabel(a, z) {
    const [what, time, why] = stretch(a, z);
    return [what ? what + " · " + time : time, why];
  }




  function renderRoute(box, b, stops, { view, canSwitch, noRaceHere }) {
    box.textContent = "";
    const median = view === "median";
    const n = (b.route_median && b.route_median.blocks) || 0;
    if (canSwitch) {
      const sw = document.createElement("div");
      sw.className = "rt-switch";
      for (const [v, label] of [["block", "This block"], ["median", `Typical \u00b7 last ${n}`]]) {
        const btn = document.createElement("button");
        btn.type = "button";
        btn.textContent = label;
        btn.className = v === view ? "on" : "";
        btn.addEventListener("click", () => setRouteView(v));
        sw.appendChild(btn);
      }
      box.appendChild(sw);
    }
    const miner = median ? "Miner" : b.pool || (b.pool_tag ? `"${b.pool_tag}"` : "unknown miner");
    const W = box.clientWidth - 12;
    // The last label is centred on its dot like the others, so the dot keeps
    // room for half of it - at the edge it looked squeezed.
    const left = ROUTE_STUB, right = W - 60;

    const need = segNeeds(box, stops);
    if (need.slice(1).reduce((x, y) => x + y, 0) > right - left) {
      // Narrow: the same stops down a line. Between two stops, what that
      // stretch is and - on the right, flush - how long it took. One rule
      // for every stretch: from the stop above, so never negative. The hop
      // into Core says in short how the compact block went; the compact
      // block line under the route says it in full, as on a wide screen.
      const ol = document.createElement("ol");
      ol.className = "rt-v";
      const li = (cls, parts, title) => {
        const e = document.createElement("li");
        e.className = cls;
        for (const [text, c] of parts) {
          const s = document.createElement(c === "name" ? "b" : "span");
          s.textContent = text;
          if (c && c !== "name") s.className = c;
          e.appendChild(s);
        }
        if (title) e.title = title;
        ol.appendChild(e);
      };
      li("stop pool", [[miner, "name"], ["mined it"]]);
      // The first job is the zero, not a station (as on the wide route):
      // when it falls inside the hop into Core - a far peer, a long hop -
      // it stands up here with how far before your node it was, and the
      // hop stays whole underneath.
      const win = stops.find((st) => st.kind === "win");
      // As on the wide route: the first job against the first station after
      // it, the peer or - with no peer to place - Core (review 11.11.2026).
      const firstStation = stops.find((st) => st.kind !== "win");
      const line = win && firstStation && win.t > firstStation.t ? stops.filter((st) => st !== win) : stops;
      if (line !== stops) {
        li("gap lead", []);
        li("stop win", [[win.name, "name"], ["first job"]]);
        li("gap lead", [[firstJobWords(win, stops)]]);
      } else li("gap lead", []);
      line.forEach((s, i) => {
        if (i > 0) {
          const prev = line[i - 1];
          // The first job's stretch to the peer says only its time here:
          // the first job's own line says what it is.
          const [what, time, why] = prev.kind === "win" ? ["", ...stretch(prev, s).slice(1)] : stretch(prev, s);
          li("gap", [[what || " "], [time, "t"]], why);
        }
        // Where the peer is stands on the card's first line; here only
        // which one, so every stop fits one line.
        const peer = s.kind === "peer" && !median;
        li("stop " + s.kind + (s.group ? " " + s.group : ""),
          [[peer ? "Your peer" : s.name, "name"], peer ? [s.name, "addr"] : [s.sub.join(" · ")]], s.title);
      });
      box.appendChild(ol);
      // "at least" in words; where it would squeeze its stretch's text past
      // two lines, the sign - a bare number would read as the estimate.
      for (const t of ol.querySelectorAll(".gap .t")) {
        if (!t.textContent.startsWith("at least")) continue;
        const what = t.previousElementSibling;
        const cs = getComputedStyle(what);
        const lh = parseFloat(cs.lineHeight) || 1.3 * parseFloat(cs.fontSize);
        if (what.getBoundingClientRect().height > 2.5 * lh) {
          t.textContent = t.textContent.replace("at least\u00a0", "≥");
        }
      }
    } else {
      const rt = document.createElement("div");
      rt.className = "rt";
      box.appendChild(rt);
      const el = (cls, x, html) => {
        const e = document.createElement("div");
        e.className = cls;
        if (x != null) e.style.left = x + "px"; // CSSOM, which the CSP allows
        if (html) e.append(...html);
        rt.appendChild(e);
        return e;
      };
      const txt = (tag, text, cls) => {
        const e = document.createElement(tag);
        e.textContent = text;
        if (cls) e.className = cls;
        return e;
      };
      const xs = placeStops(stops, left, right, need);

      const stub = el("rt-stub", 6);
      stub.style.width = (xs[0] - 6) + "px";
      const track = el("rt-track", xs[0]);
      track.style.width = (xs[xs.length - 1] - xs[0]) + "px";
      el("rt-dot pool", 6);
      // A raw coinbase text can be long; it stops short of the first stop's
      // label instead of running underneath it.
      const minerLab = el("rt-lab pool first", 0, [fit(txt("b", miner), miner), txt("small", "mined it")]);
      minerLab.style.maxWidth = (xs[0] - 50) + "px";

      stops.forEach((s, i) => {
        el("rt-dot " + s.kind + (s.group ? " " + s.group : ""), xs[i]);
        const name = fit(txt("b", s.name), s.name, s.title);
        const lab = el("rt-lab " + s.kind + (s.plain ? " plain" : ""), xs[i],
          [name, ...s.sub.map((line) => txt("small", line)), ...(s.kind === "win" ? [txt("small", firstJobWords(s, stops))] : [])]);
        if (s.title) lab.title = s.title;
      });

      // Stretches run between the stations the block actually passes -
      // peer, Core, template, your pool. The first job is the zero the times
      // are counted from, not a station: when the peer had the block before
      // any pool sent a job, the zero falls inside the ping, and splitting the
      // ping there would only produce two pieces nobody can read. So the zero
      // keeps its dot and its label below, and the stretch above it stays
      // whole. Only when the zero comes before the peer is there a stretch
      // from it to the peer.
      const at = (st) => xs[stops.indexOf(st)];
      const seg = (a, z) => {
        const [label, why] = segLabel(a, z);
        const e = el("rt-seg", (at(a) + at(z)) / 2, [txt("span", label)]);
        if (why) e.title = why;
      };
      const journey = stops.filter((st) => st.kind !== "win");
      const win = stops.find((st) => st.kind === "win");
      if (win && journey.length && win.t <= journey[0].t) seg(win, journey[0]);
      for (let i = 1; i < journey.length; i++) seg(journey[i - 1], journey[i]);
    }
    const note = document.createElement("p");
    note.className = "note";
    note.textContent = ((noRaceHere ? "No race was recorded for this block, so this is the typical route. " : "")
      + (median
      ? `Each stop is its median over the last ${n} blocks. `
      : "When the block was found cannot be measured; the hop into your node is estimated from the peer's ping.")
      ).trim();
    box.appendChild(note);
  }

  function renderBlockBody(b, routed) {
    const body = $("blockBody");
    body.textContent = "";

    // --- who brought it here - on the route when there is one ------------
    if (!routed) {
      const peers = document.createElement("div");
      const h1 = document.createElement("h3");
      h1.textContent = "Delivered first";
      peers.appendChild(h1);
      if (!(b.first_peers || []).length) {
        const p = document.createElement("p");
        p.className = "note";
        p.textContent = "No peer was credited for this block.";
        peers.appendChild(p);
      } else {
        const known = new Map((state.data.peers || []).map((p) => [p.addr, p]));
        const t = document.createElement("table");
        t.className = "firsts";
        for (const addr of b.first_peers) {
          const peer = known.get(addr);
          const tr = document.createElement("tr");
          const a = document.createElement("td");
          a.className = "mono";
          a.textContent = addr;
          const where = document.createElement("td");
          where.textContent = peer ? locLabel(peer) || "–" : "";
          const who = document.createElement("td");
          who.textContent = peer ? (peer.operator || "–") + (peer.tor_exit ? " · Tor exit" : "") : "no longer connected";
          if (!peer) who.className = "miss";
          if (peer && peer.asn) who.title = "AS" + peer.asn + (peer.tor_exit ? "\n" + TOR_EXIT_TITLE : "");
          tr.append(a, where, who);
          t.appendChild(tr);
        }
        peers.appendChild(t);
      }
      body.appendChild(peers);
    }

    // --- who turned it into work ---------------------------------------
    const race = document.createElement("div");
    const h2 = document.createElement("h3");
    h2.textContent = "Stratum race";
    race.appendChild(h2);
    if (!b.stratum || !(b.stratum.entries || []).length) {
      const p = document.createElement("p");
      p.className = "note";
      p.textContent = "No race recorded for this block.";
      race.appendChild(p);
    } else {
      const t = document.createElement("table");
      for (const e of b.stratum.entries) {
        const tr = document.createElement("tr");
        const rank = document.createElement("td");
        rank.className = "num miss";
        rank.textContent = e.rank ? e.rank + "." : "\u2013";
        const label = document.createElement("td");
        label.textContent = e.label;
        if (e.own) {
          label.className = "own";
          label.title = "Your own pool";
        }
        const delta = document.createElement("td");
        delta.className = "num" + (e.miss ? " miss" : "");
        delta.textContent = e.miss
          ? "no job"
          : e.latency_ms === 0 ? "first" : "+" + Math.round(e.latency_ms) + " ms";
        tr.append(rank, label, delta);
        t.appendChild(tr);
      }
      race.appendChild(t);
      const note = document.createElement("p");
      note.className = "note";
      note.textContent = "Times are relative to the first job seen here, not to the moment the block was found.";
      race.appendChild(note);
    }
    body.appendChild(race);
  }

  // Every <details> folds in place: its summary stays, its text rolls out
  // below and back up.
  document.addEventListener("click", (ev) => {
    const summary = ev.target.closest && ev.target.closest("details > summary");
    if (!summary || ev.target.closest("a, button, input")) return;
    ev.preventDefault();
    const box = summary.parentElement;
    foldInPlace({ anchor: summary, box, open: !box.open, set: (open) => { box.open = open; } });
  });

  // The legend and the block's pool list fold shut again with a tap anywhere
  // on them, not only on their title: on a phone the title has scrolled away
  // by then, and it comes back into view.
  for (const box of document.querySelectorAll("details.key, #blockMore")) {
    box.addEventListener("click", (ev) => {
      if (!box.open || ev.target.closest("summary, a, button, input")) return;
      if (String(window.getSelection()).length > 0) return;
      // Its title on screen: it rolls up into it, in place. Scrolled away:
      // shut at once, and the title comes back to the top of the screen.
      const summary = box.querySelector(":scope > summary");
      if (summary && summary.getBoundingClientRect().top >= 0) {
        foldInPlace({ anchor: summary, box, open: false, set: (open) => { box.open = open; } });
        return;
      }
      box.open = false;
      const top = box.getBoundingClientRect().top;
      if (top < 0) window.scrollBy(0, top - 8);
    });
  }

  // ---------- render ----------
  // "syncing" is not a kind but is picked like one: the chip beside the count.
  const SYNCING = "syncing";
  // Peers that can never bring you a block because they are not on your chain: altcoins and forks of
  // their own, and Knots nodes on a chain split off from yours (BIP110, BLAKE2b). One filter for both,
  // like syncing; the pill still says which is which.
  const FORKED = "other chain";
  const OTHER_CHAIN = "Other chain";
  const offChain = (p) => !!p.fork || p.kind === OTHER_CHAIN;
  const special = (g) => g === SYNCING || g === FORKED;
  // Under a kind its peers are picked the same way, one level down: pools by
  // name, nodes by version, everything else by the software it names.
  const POOL_KIND = "Pool node", UNKNOWN_POOL = "unknown pool", NO_NAME = "no name";
  const uaName = (p) => {
    const parts = (p.subver || "").split("/").map((s) => s.trim()).filter(Boolean);
    // "nebula/2.4.1", "Yuki / 0.0.1": the last part may be only a version.
    let i = parts.length - 1;
    while (i > 0 && /^v?\d[\w.+-]*$/.test(parts[i])) i--;
    const name = (parts[i] || "").split(/[:(]/)[0].trim();
    // "Bither1.6.2", "vds_0.9.53", "BTC RPC Proxy v0.8.1": one chip per program.
    const bare = name.replace(/(?:[\s_-]+v?|([A-Za-z]))\d+(\.\d+)+$/, "$1") || name;
    return bare === "Satoshi" ? "Core" : bare === "Schildbach Bitcoin Wallet" ? "Bitcoin Wallet" : bare || NO_NAME;
  };
  // What a node's operator wrote in brackets after the version - an
  // "(FutureBit-Apollo-Node)", a "(MiniBolt node)" - as written. Nodes only:
  // elsewhere the brackets are part of the program's own name.
  const labelOf = (p) => {
    if (!p.kind.startsWith("Node")) return null;
    const m = /\(([^)]*)\)/.exec(p.subver || "");
    return m && m[1].trim() ? m[1].trim() : null;
  };
  const subOf = (p) => {
    if (p.kind === POOL_KIND) return p.pool || UNKNOWN_POOL;
    if (p.kind.startsWith("Node")) {
      const v = /^\/Satoshi:(\d+)\.(\d+)/.exec(p.subver || "");
      // Knots names a release by the Core it is built on, down to the second
      // number (v29.3.knots20260507), and nearly all of it is on one major:
      // by the major alone every Knots peer was "29.x", one chip, none shown.
      if (v && p.kind === "Node (Knots)" && /\/Knots:/.test(p.subver)) return v[1] + "." + v[2];
      if (v) return (v[1] === "0" ? "0." + v[2] : v[1]) + ".x";
    }
    // What it pretends to be - a Core node or a wallet; who is behind it
    // (LinkingLion) is its pill's to say.
    if (p.kind === "Disguised") {
      if (/bitcoinj|breadwallet|\/bread:|multibit|bither|wallet/i.test(p.subver || "")) return "as wallet";
      return /^\/?Satoshi/i.test(p.subver || "") ? "as Core" : "as " + uaName(p);
    }
    // One program under many names (RESET), a borrowed name (Wasabi) or who
    // runs a scanner: the server knows which rule it was and names the chip.
    return p.name || uaName(p);
  };
  // Swiping back, or the back button, closes what is open - a panel, then
  // the filters - before it leaves the page, as in an app. One history entry
  // stands for "something is open"; closing it by hand takes that entry back.
  const somethingOpen = () => state.openRows.size > 0 || !!state.group || !!state.place;
  // Going back by hand can take the page back with it: Safari puts it where it
  // was when the entry was made, so clearing a filter at the top of the page
  // threw the reader down to the chip that set it (iPhone, 04.11.2026). Where
  // the reader is when they close is kept, and put back.
  let backY = null;
  function layers() {
    try {
      const ours = history.state && history.state.peermapLayer;
      if (somethingOpen() && !ours) history.pushState({ peermapLayer: true }, "");
      else if (!somethingOpen() && ours) { backY = window.scrollY; history.back(); }
    } catch { /* sandboxed frame */ }
  }
  window.addEventListener("popstate", () => {
    if (backY !== null) {
      const y = backY;
      backY = null;
      window.scrollTo(0, y);
      requestAnimationFrame(() => { if (Math.abs(window.scrollY - y) > 1) window.scrollTo(0, y); });
      return;
    }
    if (!somethingOpen()) return;
    if (state.openRows.size) state.openRows.clear();
    else { state.group = null; state.kind = null; state.sub = null; state.label = null; state.place = null; }
    applyFilters();
  });

  // On a phone the page is several screens long. Scrolling back up shows a
  // small bar with the sections - top, then each table - and it goes again
  // when the reader scrolls on down or is back at the top.
  const jumpBar = document.createElement("nav");
  jumpBar.className = "jump-bar";
  jumpBar.setAttribute("aria-label", "Jump to");
  document.body.appendChild(jumpBar);
  let lastY = window.scrollY;
  function fillJumpBar() {
    jumpBar.textContent = "";
    const go = (label, target) => {
      const b = document.createElement("button");
      b.type = "button";
      b.textContent = label;
      b.addEventListener("click", () => {
        const y = target ? target.getBoundingClientRect().top + window.scrollY - 8 : 0;
        window.scrollTo({ top: Math.max(0, y), behavior: glide() });
        jumpBar.classList.remove("shown");
      });
      jumpBar.appendChild(b);
    };
    go("\u2191 Top", null);
    for (const g of GROUPS) {
      const c = document.getElementById("group-" + g);
      if (c) go(LABEL[g], c);
    }
  }
  window.addEventListener("scroll", () => {
    const y = window.scrollY;
    const phone = window.matchMedia("(max-width: 600px)").matches;
    const up = y < lastY - 4, down = y > lastY + 4;
    // With a mouse the bar sits quietly in a corner whenever the reader is
    // far down; on a phone it comes only on the way back up.
    const show = phone ? up && y > window.innerHeight : y > window.innerHeight;
    const hide = phone ? down || y < window.innerHeight / 2 : y < window.innerHeight;
    jumpBar.classList.toggle("corner", !phone);
    if (show) {
      if (!jumpBar.classList.contains("shown")) { fillJumpBar(); jumpBar.classList.add("shown"); }
    } else if (hide) jumpBar.classList.remove("shown");
    if (up || down) lastY = y;
  }, { passive: true });

  function hoverPlace(p, on) {
    if (!window.matchMedia("(hover: hover)").matches) return;
    const g = document.querySelector(`#markers .marker[data-key="${CSS.escape(placeKey(p, renderedMode))}"]`);
    if (g) g.classList.toggle("hover", on);
  }

  // The keyboard, as in a desktop app: Esc closes what is open (a panel, then
  // the filters), the arrow keys go from row to row, Enter opens one.
  document.addEventListener("keydown", (e) => {
    if (e.target.closest && e.target.closest("input, textarea, select")) return;
    if (e.key === "Escape" && somethingOpen()) {
      if (state.openRows.size) state.openRows.clear();
      else { state.group = null; state.kind = null; state.sub = null; state.label = null; state.place = null; }
      applyFilters();
      e.preventDefault();
      return;
    }
    if (e.key !== "ArrowDown" && e.key !== "ArrowUp" && e.key !== "Enter") return;
    const rows = [...document.querySelectorAll("#tables tr[data-anchor]")].filter((r) => r.offsetParent);
    const at = rows.indexOf(document.activeElement);
    if (e.key === "Enter") {
      if (at >= 0) { rows[at].click(); e.preventDefault(); }
      return;
    }
    if (at < 0 && !(e.target === document.body || e.target === document.documentElement)) return;
    const next = at < 0 ? rows.find((r) => r.getBoundingClientRect().top >= 0) : rows[at + (e.key === "ArrowDown" ? 1 : -1)];
    if (!next) return;
    e.preventDefault();
    next.focus({ preventScroll: true });
    next.scrollIntoView({ block: "nearest", behavior: glide() });
  });

  // A tap on a chip redraws the tables; the card it sits in stays where the
  // finger left it, as in any app - the list below changes, the page does not
  // jump under the reader.
  function steady(btn, change) {
    const card = btn.closest("section[id]");
    const id = card && card.id, top = card && card.getBoundingClientRect().top;
    change();
    const now = id && document.getElementById(id);
    if (!now) return;
    const d = now.getBoundingClientRect().top - top;
    if (Math.abs(d) >= 1) window.scrollBy(0, d);
  }
  function moreChip(el, hidden, flag) {
    const b = document.createElement("button");
    b.type = "button";
    b.className = "chip kind-pick more-chips";
    b.textContent = "+ " + hidden + " more";
    b.title = "Show every chip";
    b.addEventListener("click", () => steady(b, () => { state[flag] = true; renderTables(); }));
    el.appendChild(b);
  }
  // Hides chips from the end, the picked one never, until the row and its
  // "+ n more" fit; then lets the row wrap again for the chips that are left.
  function fitOneLine(el) {
    const fits = () => el.scrollWidth <= el.clientWidth + 1;
    if (fits()) return;
    const chips = [...el.querySelectorAll("button.kind-pick")].reverse();
    moreChip(el, 0, "allSubs");
    const more = el.lastElementChild;
    let hidden = 0;
    for (const c of chips) {
      if (c.classList.contains("on")) continue;
      c.hidden = true;
      more.textContent = "+ " + ++hidden + " more";
      if (fits()) break;
    }
  }
  const SUB_LABEL = { [POOL_KIND]: "Pools:", Disguised: "Claims:", "Lightning wallet": "lnd:" };
  const subLabel = (kind) => SUB_LABEL[kind] || (kind.startsWith("Node") ? "Versions:" : "Names:");
  // Nineteen kinds were one long row of chips, several of them with a single
  // peer. The first level is three families of them and every other kind on
  // its own; the kind is the second, the name the third. A family "Other"
  // held the kind "Other", and picking it showed "Other" a second time. An
  // indexer (electrs, Fulcrum) is there for wallets, and alone it was a chip
  // for one peer, mostly the node's own.
  const groupOf = (kind) =>
    kind.startsWith("Node") || kind === POOL_KIND ? "Nodes"
    : kind === "Wallet" || kind === "Lightning wallet" || kind === "Light client" || kind === "Indexer" ? "Wallets"
    : kind === "Crawler" || kind === "Research scanner" ? "Watchers"
    : kind;
  const matchesKind = (p) => !state.group || (state.group === SYNCING ? !!p.sync_mb : state.group === FORKED ? offChain(p)
    : groupOf(p.kind) === state.group && (!state.kind || p.kind === state.kind) && (!state.sub || subOf(p) === state.sub) && (!state.label || labelOf(p) === state.label));

  const visiblePeers = () =>
    (state.data ? state.data.peers.filter((p) => state.show[p.group] && matchesKind(p)) : []);
  const regionMode = () => state.view.k >= REGION_ZOOM;

  // Every refresh rebuilds the tables and the map, and with them whatever the
  // reader had in hand: an address half selected for copying, a keyboard
  // focus, a tooltip that kept showing the old numbers. So a refresh waits
  // while something is selected, pressed or focused by keyboard - for 30 s at
  // most - and comes the moment that is over. The tooltip is closed by it.
  let pressedAt = 0;
  let heldSince = 0;
  let renderPending = false;
  const HOLD_MAX_MS = 30000;
  // Also while a finger is on the glass or the page still glides: the rows
  // the reader was following moved under it, and the scroll correction that
  // keeps their place cut Safari's glide short with a jolt (iPhone,
  // 28.10.2026). The refresh comes once the page has been still for half a
  // second.
  let touching = 0;
  let scrolledAt = 0;
  let settleTimer = 0;
  const moving = () => (touching && Date.now() - touching < 15000) || Date.now() - scrolledAt < 500;
  const settle = () => {
    clearTimeout(settleTimer);
    settleTimer = setTimeout(() => (moving() ? settle() : freed()), 520);
  };
  window.addEventListener("touchstart", () => { touching = Date.now(); }, { passive: true, capture: true });
  for (const t of ["touchend", "touchcancel"]) {
    window.addEventListener(t, () => { touching = 0; settle(); }, { passive: true, capture: true });
  }
  window.addEventListener("scroll", () => { scrolledAt = Date.now(); settle(); }, { passive: true });
  function busy() {
    if (moving()) return true;
    // A fold rolling: a refresh would rebuild what it is rolling (foldInPlace).
    if (folding) return true;
    const sel = window.getSelection && window.getSelection();
    if (sel && !sel.isCollapsed) return true;
    if (pressedAt && Date.now() - pressedAt < 2000) return true;
    const a = document.activeElement;
    return Boolean(a && a !== document.body && a.matches && a.matches(":focus-visible") && a.closest(".peers, #map"));
  }
  function renderWhenFree() {
    if (busy() && (!heldSince || Date.now() - heldSince < HOLD_MAX_MS)) {
      if (!heldSince) heldSince = Date.now();
      renderPending = true;
      return;
    }
    heldSince = 0;
    renderPending = false;
    hideTip();
    render();
  }
  const freed = () => { if (renderPending) setTimeout(renderWhenFree, 0); };
  document.addEventListener("pointerdown", () => { pressedAt = Date.now(); }, true);
  document.addEventListener("click", () => { pressedAt = 0; freed(); });
  for (const t of ["pointerup", "pointercancel"]) {
    document.addEventListener(t, () => setTimeout(() => { if (pressedAt) { pressedAt = 0; freed(); } }, 400), true);
  }
  document.addEventListener("selectionchange", freed);
  document.addEventListener("focusout", freed);
  window.addEventListener("blur", () => { pressedAt = 0; freed(); });

  // A reload puts the reader back where they were. The browser tries it before
  // the peers have arrived, finds a short page and settles near the top. So the position is kept here, set again once the
  // tables are in, and let go when the reader moves the page, when the page has
  // grown to it, or after six seconds.
  let reloadY = null;
  let restoredY = null;  // where this code last put the page
  try {
    // Always manual: with the browser's own restore on, it and this code pull the page two ways.
    if ("scrollRestoration" in history) history.scrollRestoration = "manual";
    const nav = performance.getEntriesByType("navigation")[0];
    const key = "scrollY:" + location.pathname;
    if (nav && (nav.type === "reload" || nav.type === "back_forward")) {
      const v = Number(sessionStorage.getItem(key));
      if (v > 0) reloadY = v;
    }
    // Still waiting to be restored, the page is short: keep the position that waits.
    // The filters come back with the position: a reload should not undo a pick.
    const fkey = "filters:" + location.pathname;
    if (nav && (nav.type === "reload" || nav.type === "back_forward")) {
      const f = JSON.parse(sessionStorage.getItem(fkey) || "null");
      if (f) Object.assign(state, { group: f.group || null, kind: f.kind || null, sub: f.sub || null, label: f.label || null, place: f.place || null });
    }
    const save = () => {
      try {
        sessionStorage.setItem(key, String(reloadY !== null ? reloadY : Math.round(window.scrollY)));
        sessionStorage.setItem(fkey, JSON.stringify({ group: state.group, kind: state.kind, sub: state.sub, label: state.label, place: state.place }));
      } catch { /* private window */ }
    };
    window.addEventListener("pagehide", save);
    document.addEventListener("visibilitychange", () => { if (document.hidden) save(); });
    const letGo = () => { reloadY = null; };
    for (const ev of ["wheel", "touchstart", "keydown", "mousedown"]) window.addEventListener(ev, letGo, { passive: true });
    // A drag on the scrollbar or a fling is the reader's, not ours.
    window.addEventListener("scroll", () => { if (reloadY !== null && restoredY !== null && Math.abs(window.scrollY - restoredY) > 40) letGo(); }, { passive: true });
    setTimeout(letGo, 6000);
  } catch {
    // No storage: nothing is saved, so the browser's own restore has to do.
    try { history.scrollRestoration = "auto"; } catch { /* old browser */ }
  }
  function restoreReloadY() {
    if (reloadY === null) return;
    if (state.focus) { reloadY = null; return; }
    const max = document.documentElement.scrollHeight - window.innerHeight;
    window.scrollTo(0, Math.min(reloadY, max));
    restoredY = window.scrollY;
    if (max >= reloadY) reloadY = null;
  }

  // What the reader is looking at stays where it is on a refresh. Keeping
  // the scroll position alone is not that: when anything above grows or
  // shrinks - a kind chip wrapping to another line, the block card, the list
  // of peers not on the map - everything below moves. Chrome makes up for it
  // itself (scroll anchoring); Safari does not, and the page jumped up and
  // down there on its own. The anchor is the topmost table row
  // on screen, else the topmost card.
  // A row is held by its place in its group's table, never by the peer in it
  // (28.10.2026, the same in Bitcoin Lab): sorted by ping or anything else,
  // a page that followed one peer up or down the list jumped under a reader
  // who was only looking. When a peer moves, another one stands in its
  // place; the list does not move.
  const anchorRows = (card) => [...card.querySelectorAll("tr[data-anchor]")];
  function readAnchor() {
    if (window.scrollY < 1) return null;
    const onScreen = (e) => { const r = e.getBoundingClientRect(); return r.height && r.bottom > 0 && r.top < innerHeight; };
    const e = [...document.querySelectorAll("#tables tr[data-anchor]")].find(onScreen)
      || [...document.querySelectorAll("main .card[id]")].find(onScreen);
    if (!e) return null;
    const card = e.closest(".card[id]");
    const i = e.dataset.anchor ? anchorRows(card).indexOf(e) : -1;
    return { card: card.id, i, top: e.getBoundingClientRect().top };
  }
  function holdAnchor(a) {
    if (!a) return;
    const card = document.getElementById(a.card);
    // A table that got shorter than the place: its last row stands nearest.
    const rows = card && a.i >= 0 ? anchorRows(card) : [];
    const e = card && (a.i < 0 ? card : rows[Math.min(a.i, rows.length - 1)]);
    if (!e || !e.getBoundingClientRect().height) return;
    const d = e.getBoundingClientRect().top - a.top;
    if (Math.abs(d) >= 1) window.scrollBy(0, d);
  }

  function render() {
    // Read before anything changes: rebuilding the tables makes the page
    // shorter for a moment, and Safari clamps the scroll position to it.
    const y = window.scrollY;
    const anchor = readAnchor();
    const d = state.data;
    $("demo").hidden = !d.demo;
    showError([d.error ? "Your node: " + d.error + (d.fetched_at ? " - showing the last good peer list." : "") : "", state.worldError || ""].filter(Boolean).join(" "));
    $("version").textContent = "v" + (d.version || "dev");

    const counts = { manual: 0, inbound: 0, outbound: 0 };
    for (const p of d.peers) counts[p.group]++;
    for (const g of GROUPS) document.querySelector(`[data-count="${g}"]`).textContent = counts[g];

    renderStatus();
    deliveredNow = deliveredSet();
    deliveredEver = new Set((d.last_block && d.last_block.delivered_ever) || []);
    renderSiblingLink();
    state.liveShown = Boolean(liveBlock()) || deliveredNow.size > 0;
    renderBlockCard();
    renderMarkers();
    renderUnplaced();
    renderTables();
    if (window.scrollY !== y) window.scrollTo(0, y);
    if (!folding) holdAnchor(anchor);
    restoreReloadY();
    focusOnce();
    const hash = d.last_block && d.last_block.hash;
    if (hash && state.lastHash && hash !== state.lastHash) pulseBlock();
    if (hash) state.lastHash = hash;
  }

  // Somebody arrived from Bitcoin Lab asking about one peer. Put it in front of
  // them once. Not a filter and not sticky: the rest of the node is the reason
  // they are looking at a map, and a row that keeps jumping under the cursor on
  // every ten-second refresh would be its own kind of rude.
  function focusOnce() {
    if (!state.focus || state.focusDone) return;
    const row = document.querySelector("tr.focus");
    if (!row) return;
    state.focusDone = true;
    // A card the reader folded opens for this visit, not for good: the peer
    // asked for is in it, and a reload keeps it folded as before.
    const shut = row.closest(".folded");
    if (shut) {
      shut.classList.remove("folded");
      const title = shut.querySelector(".fold-title");
      if (title) title.setAttribute("aria-expanded", "true");
      fitTables();
    }
    row.scrollIntoView({ block: "center", behavior: glide() });
  }

  // Bitcoin Lab measures which of these peers actually delivers each block
  // first. The server says whether it is installed; the port is the one its
  // store manifest publishes (or PEERMAP_SIBLING_LINK_PORT, passed on as
  // sibling_port, for a test store), and the host is this one.
  const SIBLING_PORT = 8790;
  function siblingURL(peerAddress) {
    // Bitcoin Cash Lab serves this page itself, under map/: its own page is one level up.
    const base = "../";
    return peerAddress ? base + "?peer=" + encodeURIComponent(peerAddress) : base;
  }
  function renderSiblingLink() {
    const a = $("sibling-link");
    if (!a || !state.data) return;
    // Without Bitcoin Lab the same link leads to where it can be had, as
    // Bitcoin Lab does for this app.
    const lab = !!state.data.sibling;
    a.hidden = false;
    a.target = lab ? LAB_TARGET : "_blank";
    a.href = lab ? siblingURL(null) : LAB_PAGE;
    if (lab) a.removeAttribute("title");
    else a.title = "See which of your peers bring you new blocks first";
    // The star and the green address are Bitcoin Lab's word; without it the
    // legend does not explain marks the page never shows.
    for (const li of document.querySelectorAll(".key .lab-only")) li.hidden = !lab;
  }

  // Nodes are what passes blocks on (every "Node (...)" kind and pool nodes),
  // disguised is everything pretending, LinkingLion included; the rest -
  // wallets, crawlers, indexers, unknown - are "others", so the three add up.
  // Named once, in the tooltip and the Disguised explanation, not on every card.
  const LION_CREDIT = "Tracked by 0xB10C, b10c.me";
  // Until Bitcoin Lab is in Umbrel's own store, its page is the repository.
  const LAB_PAGE = "https://github.com/benunskilled/bitcoin-lab";
  const isNode = (p) => p.kind.startsWith("Node (") || p.kind === "Pool node";
  // A dot in front of the status line beats once when fresh peers arrive.
  const liveDot = document.createElement("span");
  liveDot.className = "live-dot";
  liveDot.setAttribute("aria-hidden", "true");
  // Off again once it has beaten: the status line is rewritten every second
  // for its tooltip's clock, and putting the dot back with the class on beats it anew.
  liveDot.addEventListener("animationend", () => liveDot.classList.remove("beat"));
  function beat() {
    liveDot.classList.remove("beat");
    void liveDot.offsetWidth; // restart the animation
    liveDot.classList.add("beat");
  }

  function renderStatus() {
    const d = state.data;
    if (!d) return;
    const since = d.fetched_at ? ago(Date.now() / 1000 - d.fetched_at) + " ago" : "never";
    const mode = state.paused ? "paused" : `every ${d.interval}s while open`;
    const disguised = d.peers.filter((p) => p.kind === "Disguised" || p.linking_lion).length;
    const nodes = d.peers.filter((p) => !(p.kind === "Disguised" || p.linking_lion) && isNode(p)).length;
    const others = d.peers.length - nodes - disguised;
    const el = $("status");
    el.textContent = `${d.peers.length} peers: ${nodes} nodes, ${disguised} disguised, ${others} others`;
    // Grey whenever what is shown is not fresh: paused, unreachable, or Core
    // failing and the last good list standing in.
    liveDot.classList.toggle("stale", state.paused || state.failed || !!d.error);
    el.prepend(liveDot);
    // The dot says it is live; when and how often only on hover.
    el.title = `Updated ${since} · ${mode}`;
  }

  // ---------- map ----------
  async function loadWorld() {
    const r = await fetch("world.json");
    state.world = await r.json();
    $("map").setAttribute("viewBox", `0 0 ${state.world.w} ${state.world.h}`);
    const frag = document.createDocumentFragment();
    const addPath = (d, cc) => {
      const p = document.createElementNS(NS, "path");
      p.setAttribute("d", d);
      p.setAttribute("class", "land");
      p.setAttribute("vector-effect", "non-scaling-stroke");
      if (cc) p.dataset.cc = cc;
      frag.appendChild(p);
    };
    for (const [cc, d] of Object.entries(state.world.paths)) addPath(d, cc);
    if (state.world.unnamed) addPath(state.world.unnamed, "");
    $("countries").appendChild(frag);
    buildDots();
    applyView();
  }

  // The world view is drawn as a grid of dots rather than filled countries:
  // calmer, and the markers stand out against it. The grid is worked out once,
  // in the browser, by painting every country into an offscreen canvas in its
  // own colour and reading back which country each grid point falls on - so a
  // country with peers can light up its own dots. Zoomed in, the dots fade out
  // and the country shapes fade in (see applyView): a dot grid has nothing to
  // say about regions.
  const DOT_STEP = 4.4;
  let dotsByCC = null;
  function buildDots() {
    const W = state.world.w, H = state.world.h, S = 2;
    const off = document.createElement("canvas");
    off.width = W * S; off.height = H * S;
    const c = off.getContext("2d", { willReadFrequently: true });
    if (!c || typeof Path2D !== "function") return;
    c.scale(S, S);
    const ccs = Object.keys(state.world.paths);
    ccs.forEach((cc, i) => {
      c.fillStyle = `rgb(${(i + 1) & 255},${(i + 1) >> 8},7)`;
      c.fill(new Path2D(state.world.paths[cc]));
    });
    if (state.world.unnamed) { c.fillStyle = "rgb(0,0,9)"; c.fill(new Path2D(state.world.unnamed)); }
    const img = c.getImageData(0, 0, off.width, off.height).data;
    dotsByCC = new Map();
    let all = "";
    for (let y = DOT_STEP / 2, row = 0; y < H; y += DOT_STEP, row++) {
      for (let x = (row % 2) * DOT_STEP / 2; x < W; x += DOT_STEP) {
        const o = (Math.round(y * S) * off.width + Math.round(x * S)) * 4;
        if (img[o + 3] < 200) continue;
        const seg = `M${x.toFixed(1)} ${y.toFixed(1)}h0`;
        all += seg;
        const i = img[o] + (img[o + 1] << 8) - 1;
        if (img[o + 2] === 7 && i >= 0 && ccs[i]) dotsByCC.set(ccs[i], (dotsByCC.get(ccs[i]) || "") + seg);
      }
    }
    const grid = document.createElementNS(NS, "g");
    grid.setAttribute("class", "dots");
    for (const [id, d] of [["dotsAll", all], ["dotsHot", ""]]) {
      const p = document.createElementNS(NS, "path");
      p.id = id;
      p.setAttribute("d", d);
      grid.appendChild(p);
    }
    $("countries").appendChild(grid);
    $("map").classList.add("dotted");
  }

  // A bucket is one marker: a country (world view) or a region (zoomed in).
  // Peers whose region is unknown stay on their country's point.
  function placeKey(p, byRegion) {
    return byRegion && p.rid ? "r" + p.rid : "c" + p.cc;
  }
  function peerMatchesPlace(p) {
    if (!state.place) return true;
    const k = state.place.key;
    return k[0] === "r" ? "r" + p.rid === k : p.cc === k.slice(1);
  }

  function buckets() {
    const byRegion = regionMode();
    const delivered = deliveredNow;
    const by = new Map();
    for (const p of visiblePeers()) {
      if (!p.cc) continue;
      const key = placeKey(p, byRegion);
      let b = by.get(key);
      if (!b) {
        let pt = state.world.points[p.cc];
        let title = regionName(p.cc), sub = "";
        if (key[0] === "r") {
          pt = project(p.lon, p.lat);
          title = p.region;
          sub = regionName(p.cc);
        } else if (byRegion) {
          sub = "region unknown";
        }
        if (!pt) continue;
        by.set(key, (b = { key, title, sub, pt, cc: p.cc, manual: 0, inbound: 0, outbound: 0, total: 0 }));
      }
      b[p.group]++;
      b.total++;
      if (delivered.has(p.addr)) b.delivered = true;
    }
    return [...by.values()].sort((a, b) => b.total - a.total);
  }

  const radius = (n) => Math.min(26, 5 + 3.2 * Math.sqrt(n));

  function arcStroke(r, a0, a1) {
    if (a1 <= a0) a1 = a0 + 0.01;
    const x0 = r * Math.sin(a0), y0 = -r * Math.cos(a0);
    const x1 = r * Math.sin(a1), y1 = -r * Math.cos(a1);
    return `M${x0.toFixed(2)} ${y0.toFixed(2)}A${r} ${r} 0 ${a1 - a0 > Math.PI ? 1 : 0} 1 ${x1.toFixed(2)} ${y1.toFixed(2)}`;
  }

  let renderedMode = null;
  function renderMarkers() {
    if (!state.world || !state.data) return;
    renderedMode = regionMode();
    $("mapLevel").textContent = renderedMode ? "Regions" : "Countries";
    $("mapLevelHint").hidden = renderedMode;
    const layer = $("markers");
    layer.textContent = "";
    const bs = buckets();
    const hot = new Set(bs.map((b) => b.cc));
    for (const el of $("countries").children) el.classList.toggle("hot", hot.has(el.dataset.cc));

    // Inbound-only places first, in the background, then outbound and
    // manual on top; within each, largest first, so small markers are drawn
    // on top and stay clickable. The one that delivered the block last of
    // all, so a star is never hidden under a neighbouring country.
    const order = (b) => (b.delivered ? 3 : RANK[leadGroup(b)]);
    for (const b of [...bs].sort((x, y) => order(x) - order(y))) {
      const r = radius(b.total);
      const g = document.createElementNS(NS, "g");
      const selected = state.place && state.place.key === b.key;
      g.setAttribute("class", "marker" + (selected ? " selected" : "") + (b.delivered ? " delivered" : "") +
        (leadGroup(b) === "inbound" ? " back" : ""));
      g.dataset.x = b.pt[0];
      g.dataset.y = b.pt[1];
      g.dataset.key = b.key;
      g.setAttribute("tabindex", "0");
      g.setAttribute("role", "button");
      g.setAttribute("aria-label", `${b.title}${b.sub ? ", " + b.sub : ""}: ${GROUPS.map((k) => b[k] + " " + LABEL[k].toLowerCase()).join(", ")}`);

      // A light, not a pie: the colour is the place's leading group - manual
      // whenever it holds one of yours - and the split is in the tooltip.
      const lead = leadGroup(b);
      const glow = document.createElementNS(NS, "circle");
      glow.setAttribute("class", "glow");
      glow.setAttribute("r", r * 1.9);
      glow.setAttribute("fill", `url(#glow-${lead})`);
      g.appendChild(glow);
      const core = r * 0.38 + 1.4;
      const ring = document.createElementNS(NS, "circle");
      ring.setAttribute("class", "ring");
      ring.setAttribute("r", core + 4.2);
      g.appendChild(ring);
      // Selection gets its own outline, so a selected place that also
      // delivered the block keeps its orange ring.
      const sel = document.createElementNS(NS, "circle");
      sel.setAttribute("class", "sel");
      sel.setAttribute("r", core + 6.6);
      g.appendChild(sel);
      const dot = document.createElementNS(NS, "circle");
      dot.setAttribute("class", "core c-" + lead);
      dot.setAttribute("r", core);
      g.appendChild(dot);
      // The split between the three groups, as the pie used to show it: a
      // thin ring around the light, one arc per group, a small gap between.
      // The gap never eats a small group: at most half its own arc.
      const minSpan = Math.min(...GROUPS.filter((k) => b[k]).map((k) => (b[k] / b.total) * Math.PI * 2));
      const rr = core + 1.8, gap = b.total > 1 ? Math.min(0.32, minSpan / 2) : 0;
      let a = 0;
      for (const k of GROUPS) {
        if (!b[k]) continue;
        const span = (b[k] / b.total) * Math.PI * 2;
        const s = document.createElementNS(NS, "path");
        s.setAttribute("class", "seg seg-" + k);
        s.setAttribute("d", span >= Math.PI * 2 - 1e-6
          ? `M${-rr} 0A${rr} ${rr} 0 1 1 ${rr} 0A${rr} ${rr} 0 1 1 ${-rr} 0`
          : arcStroke(rr, a + gap / 2, a + span - gap / 2));
        g.appendChild(s);
        a += span;
      }
      if (b.total > 1) {
        const t = document.createElementNS(NS, "text");
        t.setAttribute("x", core + 5);
        t.setAttribute("y", -core);
        t.textContent = b.total;
        g.appendChild(t);
      }
      if (b.delivered) {
        const star = document.createElementNS(NS, "text");
        star.setAttribute("class", "delivered-star");
        star.setAttribute("x", 0);
        star.setAttribute("y", -(core + 6));
        star.setAttribute("text-anchor", "middle");
        star.textContent = "\u2605";
        g.appendChild(star);
      }
      g.addEventListener("pointerenter", (e) => showTip(b, e));
      g.addEventListener("pointermove", (e) => moveTip(e));
      g.addEventListener("pointerleave", hideTip);
      g.addEventListener("focus", () => showTip(b, null, g));
      g.addEventListener("blur", hideTip);
      g.addEventListener("click", (e) => { e.stopPropagation(); togglePlace(b); });
      g.addEventListener("keydown", (e) => {
        if (e.key === "Enter" || e.key === " ") { e.preventDefault(); togglePlace(b); }
      });
      layer.appendChild(g);
    }
    const hotDots = $("dotsHot");
    if (hotDots && dotsByCC) hotDots.setAttribute("d", [...hot].map((cc) => dotsByCC.get(cc) || "").join(""));
    renderLinks(bs);
    placeMarkers();
  }

  // Yours first, then the ones this node chose: a place with one outbound
  // peer among thirty inbound ones still shows as outbound. A node with an
  // open port has far more inbound peers, and by majority the map would be
  // all inbound.
  const leadGroup = (b) => (b.manual ? "manual" : b.outbound ? "outbound" : "inbound");
  const RANK = { inbound: 0, outbound: 1, manual: 2 };

  // This node itself, from the region its peers see it in (the server reads
  // getpeerinfo's addrlocal and sends only the region, never the address).
  function homePoint() {
    const h = state.data && state.data.home;
    return h ? project(h.lon, h.lat) : null;
  }

  // A thin arc from every place to this node. They make the map read as what
  // it is - your node's connections - and they are the track a new block
  // travels along (pulseBlock).
  let linkByKey = new Map();
  function arcPath(a, b) {
    const dx = b[0] - a[0], dy = b[1] - a[1];
    const d = Math.hypot(dx, dy);
    const lift = Math.min(90, d * 0.28);
    const cx = (a[0] + b[0]) / 2, cy = (a[1] + b[1]) / 2 - lift;
    return `M${a[0].toFixed(1)} ${a[1].toFixed(1)}Q${cx.toFixed(1)} ${cy.toFixed(1)} ${b[0].toFixed(1)} ${b[1].toFixed(1)}`;
  }
  function renderLinks(bs) {
    const layer = $("links");
    layer.textContent = "";
    linkByKey = new Map();
    const home = homePoint();
    if (!home) return;
    for (const b of bs) {
      if (Math.hypot(b.pt[0] - home[0], b.pt[1] - home[1]) < 2) continue;
      const p = document.createElementNS(NS, "path");
      p.setAttribute("class", "link l-" + leadGroup(b));
      p.setAttribute("d", arcPath(b.pt, home));
      p.setAttribute("vector-effect", "non-scaling-stroke");
      layer.appendChild(p);
      linkByKey.set(b.key, p);
    }
    const g = document.createElementNS(NS, "g");
    g.setAttribute("class", "home-mark");
    g.dataset.x = home[0];
    g.dataset.y = home[1];
    const halo = document.createElementNS(NS, "circle");
    halo.setAttribute("r", 11);
    halo.setAttribute("fill", "url(#glow-home)");
    const dot = document.createElementNS(NS, "circle");
    dot.setAttribute("r", 3.2);
    dot.setAttribute("class", "home-dot");
    const t = document.createElementNS(NS, "title");
    t.textContent = "Your node";
    g.append(halo, dot, t);
    $("markers").appendChild(g);
  }

  // A new block: a light runs from the place that delivered it along its arc
  // to this node, rings spread where it lands, and the wave rolls across the
  // page. Only for a block that arrives while the page is open - not on the
  // first load - and not at all for someone who asked for less motion.
  const motionQuery = window.matchMedia ? window.matchMedia("(prefers-reduced-motion: reduce)") : null;
  function pulseBlock() {
    if ((motionQuery && motionQuery.matches) || document.hidden || !state.world) return;
    const wave = $("blockWave");
    wave.classList.remove("run"); void wave.offsetWidth; wave.classList.add("run");
    const home = homePoint();
    const fx = $("fx");
    const svg = $("map");
    // Sizes in screen pixels: the fx layer is scaled with the map.
    const px = (n) => (n * state.world.w) / (svg.clientWidth || 1000) / state.view.k;
    const from = [...$("markers").querySelectorAll(".marker.delivered")].map((g) => linkByKey.get(g.dataset.key)).filter(Boolean);
    const land = () => {
      if (!home) return;
      for (let i = 0; i < 3; i++) ring(home, i * 220);
    };
    const ring = (pt, delay) => {
      const c = document.createElementNS(NS, "circle");
      c.setAttribute("class", "fx-ring");
      c.setAttribute("cx", pt[0]);
      c.setAttribute("cy", pt[1]);
      c.setAttribute("vector-effect", "non-scaling-stroke");
      fx.appendChild(c);
      const t0 = performance.now() + delay;
      const step = (now) => {
        const k = Math.max(0, (now - t0) / 1500);
        c.setAttribute("r", px(3 + 70 * (1 - Math.pow(1 - Math.min(1, k), 3))));
        c.style.opacity = String(Math.max(0, 1 - k));
        if (k < 1) requestAnimationFrame(step); else c.remove();
      };
      requestAnimationFrame(step);
    };
    if (!from.length) { land(); return; }
    let left = from.length;
    for (const path of from) {
      const len = path.getTotalLength();
      const head = document.createElementNS(NS, "circle");
      head.setAttribute("class", "fx-head");
      fx.appendChild(head);
      // Without non-scaling-stroke: the dash lengths must be in the same units
      // as getTotalLength(), so the width is scaled by hand instead.
      const trail = path.cloneNode();
      trail.removeAttribute("vector-effect");
      trail.setAttribute("class", "fx-trail");
      trail.style.strokeWidth = String(px(2.2));
      fx.appendChild(trail);
      const t0 = performance.now();
      const step = (now) => {
        const k = Math.min(1, (now - t0) / 950), e = 1 - Math.pow(1 - k, 2.2), d = e * len;
        const pt = path.getPointAtLength(d);
        head.setAttribute("cx", pt.x); head.setAttribute("cy", pt.y); head.setAttribute("r", px(2.6));
        trail.setAttribute("stroke-dasharray", `${len * 0.22} ${len}`);
        trail.setAttribute("stroke-dashoffset", String(-(d - len * 0.22)));
        if (k < 1) { requestAnimationFrame(step); return; }
        head.remove(); trail.remove();
        if (--left === 0) land();
      };
      requestAnimationFrame(step);
    }
  }

  function placeMarkers() {
    const { k, x, y } = state.view;
    // Markers keep their on-screen size while the map zooms: they are
    // positioned in viewBox units but scaled by the inverse of the fit ratio.
    const svg = $("map");
    const fit = svg.clientWidth ? state.world.w / svg.clientWidth : 1;
    // On a phone the whole map is ~360px wide; full-size markers would bury Europe.
    const shrink = Math.min(1, Math.max(0.55, (svg.clientWidth || 800) / 800));
    for (const g of $("markers").children) {
      const px = +g.dataset.x * k + x, py = +g.dataset.y * k + y;
      g.setAttribute("transform", `translate(${px.toFixed(1)} ${py.toFixed(1)}) scale(${(fit * shrink).toFixed(3)})`);
    }
  }

  function togglePlace(b) {
    state.place = state.place && state.place.key === b.key ? null : { key: b.key, label: b.sub && b.key[0] === "r" ? `${b.title}, ${b.sub}` : b.title };
    renderPlaceFilter();
    renderMarkers();
    renderTables();
  }
  function renderPlaceFilter() {
    $("placeFilter").hidden = !state.place;
    $("placeFilterName").textContent = state.place ? state.place.label : "";
    $("kindFilter").hidden = !state.group;
    $("kindFilterName").textContent = [state.group, state.kind !== state.group && state.kind, state.sub, state.label].filter(Boolean).join(" \u00b7 ");
  }

  // Both filters redraw the same three things, and the map is in that list on
  // purpose: after clicking "5 Pool node" the next question is where those
  // five are, and the map answers it without a second click.
  function applyFilters() {
    layers();
    renderPlaceFilter();
    renderUnplaced();
    renderMarkers();
    renderTables();
  }

  function setGroup(group) {
    state.group = state.group === group ? null : group;
    state.kind = null;
    state.sub = null;
    state.label = null;
    state.allSubs = false;
    applyFilters();
  }
  function setKind(kind) {
    state.kind = state.kind === kind ? null : kind;
    state.sub = null;
    state.label = null;
    state.allSubs = false;
    applyFilters();
  }
  function setSub(sub) {
    state.sub = state.sub === sub ? null : sub;
    applyFilters();
  }
  function setLabel(label) {
    state.label = state.label === label ? null : label;
    applyFilters();
  }

  $("placeFilterClear").addEventListener("click", () => {
    state.place = null;
    applyFilters();
  });
  $("kindFilterClear").addEventListener("click", () => {
    state.group = null;
    state.kind = null;
    state.sub = null;
    state.label = null;
    applyFilters();
  });

  // ---------- tooltip ----------
  const tip = $("tooltip");
  function showTip(b, e, el) {
    tip.textContent = "";
    const h = document.createElement("h3");
    h.textContent = b.title;
    tip.appendChild(h);
    if (b.sub) {
      const s = document.createElement("div");
      s.className = "sub";
      s.textContent = b.sub;
      tip.appendChild(s);
    }
    for (const k of GROUPS) {
      if (!state.show[k]) continue;
      const row = document.createElement("div");
      row.className = "row";
      const sw = document.createElement("span");
      sw.className = "swatch s-" + k;
      const label = document.createElement("span");
      label.textContent = LABEL[k];
      const n = document.createElement("b");
      n.textContent = b[k];
      row.append(sw, label, n);
      tip.appendChild(row);
    }
    tip.hidden = false;
    if (e) moveTip(e);
    else if (el) {
      const wr = $("mapWrap").getBoundingClientRect(), r = el.getBoundingClientRect();
      positionTip(r.right - wr.left, r.top - wr.top);
    }
  }
  function moveTip(e) {
    const wr = $("mapWrap").getBoundingClientRect();
    positionTip(e.clientX - wr.left, e.clientY - wr.top);
  }
  function positionTip(x, y) {
    const wrap = $("mapWrap");
    const w = tip.offsetWidth, h = tip.offsetHeight;
    let left = x + 14, top = y + 14;
    if (left + w > wrap.clientWidth - 8) left = x - w - 14;
    if (top + h > wrap.clientHeight - 8) top = y - h - 14;
    tip.style.left = Math.max(8, left) + "px";
    tip.style.top = Math.max(8, top) + "px";
  }
  function hideTip() { tip.hidden = true; }

  // ---------- pan & zoom ----------
  const MAX_K = 24;
  function clampView() {
    const v = state.view, W = state.world.w, H = state.world.h;
    v.k = Math.min(MAX_K, Math.max(1, v.k));
    v.x = Math.min(0, Math.max(W - W * v.k, v.x));
    v.y = Math.min(0, Math.max(H - H * v.k, v.y));
  }
  function applyView() {
    if (!state.world) return;
    clampView();
    const { k, x, y } = state.view;
    for (const id of ["countries", "links", "fx"]) $(id).setAttribute("transform", `translate(${x} ${y}) scale(${k})`);
    // Dots at the world view, country shapes once zoomed in towards regions.
    $("map").style.setProperty("--dots", String(Math.max(0, Math.min(1, (REGION_ZOOM + 0.5 - k) / 1.5))));
    if (!state.data) return;
    if (renderedMode !== regionMode()) {
      hideTip();
      renderMarkers(); // crossing the region threshold regroups the markers
    } else {
      placeMarkers();
    }
  }
  function toSvg(clientX, clientY) {
    const svg = $("map");
    const pt = svg.createSVGPoint();
    pt.x = clientX; pt.y = clientY;
    return pt.matrixTransform(svg.getScreenCTM().inverse());
  }
  function zoomAt(factor, sx, sy) {
    const v = state.view;
    const nk = Math.min(MAX_K, Math.max(1, v.k * factor));
    const f = nk / v.k;
    v.x = sx - (sx - v.x) * f;
    v.y = sy - (sy - v.y) * f;
    v.k = nk;
    applyView();
  }
  function setupPanZoom() {
    const svg = $("map");
    svg.addEventListener("wheel", (e) => {
      // Zoomed all the way out, turning the wheel further out is the page's:
      // over a map that fills the screen, scrolling past it was impossible.
      // A trackpad pinch arrives as a wheel with ctrlKey: still the map's, or
      // the browser zooms the whole page.
      if (state.view.k <= 1 && e.deltaY > 0 && !e.ctrlKey) return;
      e.preventDefault();
      const p = toSvg(e.clientX, e.clientY);
      zoomAt(Math.exp(-e.deltaY * 0.0015), p.x, p.y);
    }, { passive: false });

    const pointers = new Map();
    let last = null, pinch = null, moved = false;
    svg.addEventListener("pointerdown", (e) => {
      if (e.target.closest(".marker")) return;
      svg.setPointerCapture(e.pointerId);
      pointers.set(e.pointerId, toSvg(e.clientX, e.clientY));
      moved = false;
      if (pointers.size === 1) { last = pointers.get(e.pointerId); svg.classList.add("dragging"); }
      if (pointers.size === 2) {
        const [a, b] = [...pointers.values()];
        pinch = { d: Math.hypot(a.x - b.x, a.y - b.y) };
      }
    });
    svg.addEventListener("pointermove", (e) => {
      if (!pointers.has(e.pointerId)) return;
      const p = toSvg(e.clientX, e.clientY);
      pointers.set(e.pointerId, p);
      if (pointers.size === 2 && pinch) {
        const [a, b] = [...pointers.values()];
        const d = Math.hypot(a.x - b.x, a.y - b.y);
        if (pinch.d > 0) zoomAt(d / pinch.d, (a.x + b.x) / 2, (a.y + b.y) / 2);
        pinch.d = d;
        moved = true;
      } else if (pointers.size === 1 && last) {
        state.view.x += p.x - last.x;
        state.view.y += p.y - last.y;
        if (Math.abs(p.x - last.x) + Math.abs(p.y - last.y) > 0.5) moved = true;
        applyView();
        last = toSvg(e.clientX, e.clientY);
      }
    });
    const end = (e) => {
      pointers.delete(e.pointerId);
      if (pointers.size < 2) pinch = null;
      if (pointers.size === 0) { last = null; svg.classList.remove("dragging"); }
      else last = [...pointers.values()][0];
    };
    svg.addEventListener("pointerup", end);
    svg.addEventListener("pointercancel", end);
    svg.addEventListener("dblclick", (e) => {
      const p = toSvg(e.clientX, e.clientY);
      zoomAt(2, p.x, p.y);
    });
    svg.addEventListener("click", () => {
      if (!moved && state.place) {
        state.place = null;
        applyFilters();
      }
    });

    const center = () => [state.world.w / 2, state.world.h / 2];
    $("zoomIn").addEventListener("click", () => zoomAt(1.6, ...center()));
    $("zoomOut").addEventListener("click", () => zoomAt(1 / 1.6, ...center()));
    $("zoomReset").addEventListener("click", () => { state.view = { k: 1, x: 0, y: 0 }; applyView(); });
    window.addEventListener("resize", () => { if (state.data) placeMarkers(); });
    // The route is laid out in pixels from the card's width, so a new width
    // needs a new layout - and a narrow one may need the list instead.
    let routeResize = null, routeWidth = window.innerWidth;
    window.addEventListener("resize", () => {
      if (window.innerWidth === routeWidth) return;
      routeWidth = window.innerWidth;
      clearTimeout(routeResize);
      routeResize = setTimeout(() => { if (state.data) renderBlockCard(); }, 150);
    });
  }

  // ---------- filters ----------
  for (const btn of document.querySelectorAll(".chip[data-group]")) {
    btn.addEventListener("click", () => {
      const g = btn.dataset.group;
      state.show[g] = !state.show[g];
      btn.setAttribute("aria-pressed", String(state.show[g]));
      if (state.data) render();
    });
  }

  // ---------- not on the map (collapsible side panel) ----------
  function setSide(open) {
    $("unplaced").hidden = !open;
    $("sideToggle").setAttribute("aria-expanded", String(open));
    store.set(SIDE_KEY, open ? "1" : "0");
    if (state.world && state.data) placeMarkers(); // map width changed
  }
  $("sideToggle").addEventListener("click", () => setSide($("unplaced").hidden));
  setSide(store.get(SIDE_KEY) === "1"); // collapsed unless opened before

  function renderUnplaced() {
    const rows = new Map();
    let total = 0;
    for (const p of state.data.peers) {
      if (p.cc || !matchesKind(p)) continue;
      let key = netLabel(p.network);
      if (p.network === "ipv4" || p.network === "ipv6") key = "No geo data";
      let r = rows.get(key);
      if (!r) rows.set(key, (r = { manual: 0, inbound: 0, outbound: 0 }));
      r[p.group]++;
      if (state.show[p.group]) total++;
    }
    $("unplacedCount").textContent = total;
    const body = $("unplacedBody");
    body.textContent = "";
    if (!rows.size) {
      const tr = document.createElement("tr");
      const td = document.createElement("td");
      td.colSpan = 4;
      td.textContent = "Every peer is on the map.";
      tr.appendChild(td);
      body.appendChild(tr);
      return;
    }
    for (const [name, r] of [...rows.entries()].sort()) {
      const tr = document.createElement("tr");
      const th = document.createElement("td");
      th.textContent = name;
      tr.appendChild(th);
      for (const g of GROUPS) {
        const td = document.createElement("td");
        td.textContent = state.show[g] ? r[g] : "–";
        tr.appendChild(td);
      }
      body.appendChild(tr);
    }
  }

  // ---------- tables ----------

  // The kind is what the peer calls itself, the flags are what Core observed.
  // A claim that does not fit what was observed shows as the reason under the
  // pill (p.tell); the flags themselves are in the panel.
  // A node catching up from this one: a turning arrow, the word and what it
  // has taken. Always first in the grey line under the place.
  // What "no headers" means, on hover: Core never got a block header from the peer.
  // BIP159: NETWORK_LIMITED alone promises only the last 288 blocks. Every
  // Core since 0.16 sets it next to NETWORK, so only a node without NETWORK is
  // pruned. Said of nodes only: a disguised peer's bits are as made up as its name.
  const PRUNED_WHY = "Keeps only recent blocks";
  // The one line a filtered phone card has: the most important thing about
  // the peer that the pill does not say already (09.11.2026). In the syncing
  // filter, what put it there; else whose pool; why a disguise is one, what
  // it did before what it says; for a node another chain, or the software
  // where the pill says only "other", or whether it keeps every block; for the
  // rest what it did, then what it is, and its user agent where nothing is
  // known. Never empty, so every card in a filter is one height.
  const probablyOf = (p) => ((p.about || "").split("\n").find((l) => l.startsWith("Probably: ")) || "").slice("Probably: ".length);
  const cardLine = (p) => {
    if (p.sync_mb) return "syncing " + mbText(p.sync_mb);
    if (p.kind === POOL_KIND) return (p.pool || UNKNOWN_POOL) + (/block relay/i.test(p.about || "") ? " \u00b7 block relay" : "");
    if (p.linking_lion) return p.tell || "on 0xB10C's list";
    if (p.kind === "Disguised") return p.tell || probablyOf(p);
    if (p.kind.startsWith("Node")) {
      if (p.fork) return p.fork;
      // A known block relay's address: whose relay it is (peers.go, relayOf).
      if (p.tell) return p.tell;
      if (p.kind === "Node (other)" && p.label) return p.label;
      const s = p.services || [];
      return s.includes("NETWORK") ? "full node" : s.includes("NETWORK_LIMITED") ? "pruned" : s.length ? "serves no blocks" : "";
    }
    return p.tell || p.label || probablyOf(p) || (p.about || "").split("\n")[0] || p.subver || "";
  };
  const isPruned = (p) => p.kind.startsWith("Node") && !!p.services && p.services.includes("NETWORK_LIMITED") && !p.services.includes("NETWORK");
  const NO_HEADERS_WHY = "This peer has never sent your node a block header, so your node cannot tell which chain it follows";
  function mbText(mb) {
    return mb >= 1000 ? (mb / 1000).toFixed(1) + " GB" : mb + " MB";
  }
  function syncSpan(p) {
    const s = document.createElement("span");
    s.className = "syncing";
    // Core counts bytes per connection, so this is the session, not the whole sync.
    s.title = `Syncing from your node: ${mbText(p.sync_mb)} of blocks since it connected`;
    const ico = document.createElement("span");
    ico.className = "sync-ico";
    ico.setAttribute("aria-hidden", "true");
    ico.textContent = "\u21bb";
    s.append(ico, " syncing " + mbText(p.sync_mb));
    return s;
  }


  // The mix under a heading counts what is in the table - and each count is
  // the way to those peers. Clicking "5 Pool node" shows exactly those five,
  // here and on the map; clicking it again puts everyone back.
  function renderKindMix(el, peers) {
    const n = new Map();
    // Altcoins are in the "other chain" chip beside these, not a group of their own.
    for (const p of peers) if (p.kind !== OTHER_CHAIN) n.set(groupOf(p.kind), (n.get(groupOf(p.kind)) || 0) + 1);
    const entries = [...n].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
    // Five groups fit a phone in two lines: no "+ n more" here.
    entries.forEach(([group, count]) => {
      // With one group picked the others step aside: on a phone they would
      // fill the screen above the chips that matter now.
      if (state.group && state.group !== group) return;
      const b = document.createElement("button");
      b.type = "button";
      b.className = "chip kind-pick" + (state.group === group ? " on" : "");
      const i = document.createElement("i");
      i.textContent = count;
      b.append(i, " " + group);
      b.title = state.group === group ? "Show every kind again" : `Show only ${group}`;
      b.setAttribute("aria-pressed", String(state.group === group));
      b.addEventListener("click", () => steady(b, () => setGroup(group)));
      el.appendChild(b);
    });
  }

  // With a group picked, the kinds in it, when there is more than one: the
  // second level, laid out like the names below it.
  function renderKindPicks(card, peers) {
    const n = new Map();
    for (const p of peers) n.set(p.kind, (n.get(p.kind) || 0) + 1);
    if (n.size < 2 && !state.kind) return;
    const el = document.createElement("div");
    el.className = "kinds chips pool-picks";
    for (const [kind, count] of [...n].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))) {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "chip kind-pick" + (state.kind === kind ? " on" : "");
      const i = document.createElement("i");
      i.textContent = count;
      b.append(i, " " + kind);
      b.title = state.kind === kind ? "Show the whole group again" : `Show only ${kind}`;
      b.setAttribute("aria-pressed", String(state.kind === kind));
      b.addEventListener("click", () => steady(b, () => setKind(kind)));
      el.appendChild(b);
    }
    card.appendChild(el);
  }

  // With a kind picked, what those peers are one level down: one chip per
  // pool, version or name, the unknown ones last. Nothing to pick from one.
  function renderSubMix(card, kind, peers) {
    const n = new Map();
    for (const p of peers) n.set(subOf(p), (n.get(subOf(p)) || 0) + 1);
    if (n.size < 2 && !state.sub) return;
    // Largest first, like the kinds above: with one line to show, the largest
    // are the ones to see. What is not known by name comes last.
    const last = (s) => s === UNKNOWN_POOL || s === NO_NAME;
    const entries = [...n].sort((a, b) => last(a[0]) - last(b[0]) || b[1] - a[1] || a[0].localeCompare(b[0]));
    const el = document.createElement("div");
    el.className = "kinds chips pool-picks";
    const label = document.createElement("span");
    label.className = "picks-label";
    label.textContent = subLabel(kind);
    el.appendChild(label);
    for (const [sub, count] of entries) {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "chip kind-pick" + (state.sub === sub ? " on" : "");
      const i = document.createElement("i");
      i.textContent = count;
      b.append(i, " " + sub);
      b.title = state.sub === sub ? "Show all of them again" : `Show only ${sub}`;
      b.setAttribute("aria-pressed", String(state.sub === sub));
      b.addEventListener("click", () => steady(b, () => setSub(sub)));
      el.appendChild(b);
    }
    card.appendChild(el);
    // One line only: what does not fit waits behind "+ n more", fitted in
    // renderTables before the page is drawn, so nothing flickers.
    if (!state.allSubs) el.classList.add("one-line");
  }

  // The operators' own labels on these nodes, those that more than one node
  // carries: one chip each, beside the versions. A label on a single node is
  // somebody's name for their box, not something to filter by.
  function renderLabelMix(card, peers) {
    const n = new Map();
    for (const p of peers) {
      const l = labelOf(p);
      if (l) n.set(l, (n.get(l) || 0) + 1);
    }
    const entries = [...n].filter(([l, c]) => c > 1 || l === state.label).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]));
    if (!entries.length) return;
    const el = document.createElement("div");
    el.className = "kinds chips pool-picks";
    const head = document.createElement("span");
    head.className = "picks-label";
    head.textContent = "Labels:";
    el.appendChild(head);
    for (const [l, count] of entries) {
      const b = document.createElement("button");
      b.type = "button";
      b.className = "chip kind-pick" + (state.label === l ? " on" : "");
      const i = document.createElement("i");
      i.textContent = count;
      b.append(i, " " + l);
      b.title = state.label === l ? "Show all of them again" : `Show only the nodes labelled ${l}`;
      b.setAttribute("aria-pressed", String(state.label === l));
      b.addEventListener("click", () => steady(b, () => setLabel(l)));
      el.appendChild(b);
    }
    card.appendChild(el);
  }

  // How spread out a group is: distinct operators and countries. Operators by
  // the name in the Operator column, not by ASN: a large company runs several
  // ASNs (Amazon has AS16509 and AS14618) and should still count once. Peers
  // that cannot be placed - Tor, I2P, private addresses - have no country, and
  // are named instead of being silently left out. Your Umbrel's own network
  // has an operator line but is no provider, so it is not counted as one.
  function spreadText(peers) {
    const ops = new Set(), ccs = new Set();
    let unplaced = 0;
    for (const p of peers) {
      if ((p.operator || p.asn) && p.network !== "not_publicly_routable") ops.add(p.operator || "AS" + p.asn);
      if (p.cc) ccs.add(p.cc);
      else unplaced++;
    }
    const parts = [];
    if (ops.size) parts.push(ops.size + (ops.size === 1 ? " provider" : " providers"));
    if (ccs.size) parts.push(ccs.size + (ccs.size === 1 ? " country" : " countries"));
    if (unplaced) parts.push(unplaced + " not placed");
    return parts.join(" \u00b7 ");
  }

  const TRANSPORT_TITLE = {
    v1: "Unencrypted - the original P2P protocol",
    v2: "Encrypted (BIP324) - an observer on the line cannot read what passes between the nodes",
  };

  // Two lines per peer, as in the approved design: what a row is about on
  // top, the detail under it - the software, the AS number, the address -
  // so nothing needs a pointer to be read.
  const placeTitle = (p) => p.network === "not_publicly_routable" ? (p.app || "This Umbrel")
    : p.region || (p.cc ? regionName(p.cc) : netLabel(p.network));
  const neverNote = (p) => !p.no_relay ? ""
    : p.fork ? "On a chain of its own, not yours - it can never deliver one of your blocks first."
    : p.no_services ? "Offers no services, so it has no blocks to pass on - it can never deliver one first."
    : "Software that does not pass blocks on - this peer can never deliver one first.";
  const line = (cls, text) => {
    const e = document.createElement("small");
    e.className = cls;
    e.textContent = text;
    return e;
  };

  const COLS = [
    {
      // Where it is on top, the country and the address under it. Sorted by
      // country first - otherwise Bavaria and Hesse end up on either side of
      // England. Peers without a place sort last.
      key: "peer", label: "Peer", cls: "peer",
      val: (p) => (p.cc ? regionName(p.cc) + "\u0000" + (p.region || "") : "\uffff" + p.addr),
      cell: (td, p) => {
        const who = document.createElement("div");
        who.className = "who";
        // A small arrow at the left: the row opens on a tap or a click.
        // A mouse or finger hits the row; the keyboard reaches the same thing here.
        const chev = document.createElement("button");
        chev.type = "button";
        chev.className = "chev";
        chev.setAttribute("aria-label", "Show everything about this peer");
        chev.setAttribute("aria-expanded", "false");
        chev.textContent = "\u203a";
        who.appendChild(chev);
        const f = document.createElement("span");
        f.className = "flag";
        f.textContent = HIDDEN_FLAG[p.network] || flagOf(p.cc);
        const text = document.createElement("div");
        text.className = "who-text";
        const title = placeTitle(p);
        const b = fit(document.createElement("b"), title);
        b.textContent = title;
        if (HIDDEN[p.network]) b.dataset.tip = HIDDEN[p.network].tip;
        const sub = document.createElement("small");
        if (p.region && p.cc) {
          sub.append(regionName(p.cc) + " ");
        }
        // What the row colour means, said where the peer is named rather
        // than over every cell of the row.
        const got = deliveredNow.has(p.addr) ? "Delivered the newest block first"
          : deliveredEver.has(p.addr) ? "Has delivered a block first (Bitcoin Cash Lab)" : "";
        fit(sub, p.addr, got);
        if (deliveredNow.has(p.addr)) {
          const star = document.createElement("span");
          star.className = "star";
          star.textContent = "\u2605";
          star.title = "Delivered the block that just landed";
          sub.appendChild(star);
        }
        // With Bitcoin Lab installed the address is the way over: same peer,
        // the other question. Without it, plain text - no dead links.
        if (state.data && state.data.sibling) {
          const a = document.createElement("a");
          a.className = "peer-jump addr-txt";
          a.href = siblingURL(p.addr);
          a.target = LAB_TARGET;
          a.textContent = p.addr;
          a.dataset.tip = "Open this peer in Bitcoin Cash Lab";
          a.title = [got, a.dataset.tip].filter(Boolean).join("\n");
          sub.appendChild(a);
        } else {
          const a = document.createElement("span");
          a.className = "addr-txt";
          a.textContent = p.addr;
          sub.appendChild(a);
        }
        text.append(b, sub);
        who.append(f, text);
        td.appendChild(who);
      },
    },
    {
      // Whose machine it is, with the AS number under it. Sorting by this
      // column catches three peers in three countries with the same company.
      key: "operator", label: "Operator", cls: "op", val: (p) => p.operator || "\uffff",
      cell: (td, p) => {
        if (!p.operator && !p.asn) { td.textContent = "–"; td.classList.add("dim"); return; }
        const name = fit(document.createElement("span"), p.operator || "");
        name.className += " op-name";
        name.textContent = p.operator || "";
        td.appendChild(name);
        // The AS number stays faint; "Tor exit" is bright, because it changes
        // what the row means: the place and operator are the exit's.
        if (p.asn || p.tor_exit) {
          const e = line("sub", p.asn ? "AS" + p.asn + (p.tor_exit ? " · " : "") : "");
          if (p.tor_exit) {
            const tor = document.createElement("span");
            tor.className = "tor-exit";
            tor.textContent = "Tor exit";
            e.append(tor);
          }
          td.appendChild(e);
        }
        if (p.tor_exit) td.title = TOR_EXIT_TITLE;
      },
    },
    {
      // What it calls itself and what Core saw, as a pill; the software it
      // announced under it. A peer that can never deliver a block first is
      // dimmed and says so in the pill - not a fault, a wallet is doing what
      // a wallet does.
      key: "kind", label: "Kind", val: (p) => p.kind || "",
      cell: (td, p) => {
        const head = document.createElement("span");
        head.className = "kind-head";
        const marks = document.createElement("span");
        marks.className = "hb-marks";
        head.appendChild(marks);
        for (const [active, symbol, title] of [[p.hb_to, "⚡", HB_TITLE], [p.hb_from, "📡", HB_FROM_TITLE]]) {
          if (!active) continue;
          const mark = document.createElement("span");
          mark.className = "hb";
          mark.textContent = symbol;
          mark.title = title;
          mark.setAttribute("role", "img");
          mark.setAttribute("aria-label", title);
          marks.appendChild(mark);
        }
        const pill = document.createElement("span");
        // Green for every peer that can deliver a block first, grey for those
        // that never can, red for a disguise or LinkingLion even when it could.
        // LinkingLion has its own pill whatever software it claims; the
        // software stays in the line under it.
        pill.className = "kind " + (p.linking_lion ? "lion" : p.kind === "Disguised" ? "disg" : p.no_relay ? "never" : "first");
        // The pill says the kind and nothing else, for every peer: what Core
        // saw (no services, no tx, no headers) is in the panel. Software that
        // passes no blocks on gets the ⊘ and a muted row; "never first" is
        // said on hover and in the panel (28.10.2026: beside "offers no
        // services" it said the same twice, and without Bitcoin Lab next door
        // nothing in the map says what "first" is).
        // One layout in every row, and the pill is never cut.
        const label = p.linking_lion ? "LinkingLion" : p.kind;
        if (p.no_relay) {
          const ico = document.createElement("span");
          ico.className = "ico";
          ico.textContent = "⊘";
          pill.append(ico, " ");
        }
        pill.append(label);
        fit(pill, label, [p.about, p.seen ? "In this session: " + p.seen : "", p.linking_lion ? LION_CREDIT : "", neverNote(p), p.fork_why].filter(Boolean).join("\n"));
        head.appendChild(pill);
        td.appendChild(head);
        // The grey line under the pill, on a wide screen only, is for what
        // the pill cannot say: why a peer is Disguised, or what an unknown or
        // nameless one does (p.tell); which pool a pool node is; that a node
        // is syncing from this one, keeps only recent blocks or enforces rules
        // of its own (fork) - its own settings, whoever is faster. What else Core saw (no tx, no headers)
        // is normal for many peers and only in the panel, with why
        // (28.10.2026: one line for the reason, not a list of flags). A phone
        // card is the place, the address and the pill and nothing more.
        // One grey line under the pill, the same words on every width: the
        // most important thing about the peer that the pill does not say
        // (cardLine). A wide table always has it (11.11.2026: "Wallet" alone
        // did not say which); a phone card only with a filter on, so that
        // unfiltered every card stays two lines (09.11.2026). One line,
        // never wrapped - a kind cell is two lines at most.
        {
          const e = document.createElement("small");
          e.className = "card-line" + (state.group ? "" : " unfiltered");
          // Nothing known still takes its line, or the cards would differ.
          const text = cardLine(p) || "\u00a0";
          e.textContent = text;
          // Syncing keeps its turning arrow; a fork and pruned say why on hover.
          if (p.sync_mb) e.replaceChildren(syncSpan(p));
          // On hover: the whole line where it is cut, and why - the reason's
          // whole sentence (p.why), a fork's or pruned's meaning (fit).
          const why = p.fork && text === p.fork ? p.fork_why : text === "pruned" ? PRUNED_WHY : p.why || "";
          if (!p.sync_mb) fit(e, text, why);
          // A second part stays only where it fits whole (fitTables).
          const two = text.split(" \u00b7 ");
          if (two.length > 1) Object.assign(e.dataset, { full: text, one: two[0] });
          td.appendChild(e);
        }
      },
    },
    {
      // Network and P2P version as two small tags; outbound also its type.
      key: "net", label: "Net", val: (p) => netLabel(p.network) + (p.transport || ""),
      cell: (td, p) => {
        const tag = (text, title, cls) => {
          const e = document.createElement("span");
          e.className = cls ? "net " + cls : "net";
          e.textContent = text;
          if (title) e.title = title;
          td.appendChild(e);
        };
        tag(HIDDEN[p.network] ? HIDDEN[p.network].short : netLabel(p.network) || "–",
          HIDDEN[p.network] ? HIDDEN[p.network].tip : "");
        if (p.transport) tag(p.transport, TRANSPORT_TITLE[p.transport] || "");
        if (p.group === "outbound" && p.type) tag(typeLabel(p.type), "", "type");
      },
    },
    {
      key: "ping", label: "Ping", cls: "num", val: (p) => (p.min_ping_ms == null ? Infinity : p.min_ping_ms),
      cell: (td, p) => {
        if (p.min_ping_ms == null) { td.textContent = "–"; td.classList.add("dim"); return; }
        td.textContent = fmtPing(p.min_ping_ms);
      },
    },
    { key: "conntime", label: "Connected", cls: "num", val: (p) => p.conntime, show: (p) => ago(Date.now() / 1000 - p.conntime) },
  ];

  // What is known of a peer, in full: the sentences behind the words in its
  // row, what it announced, the connection, the traffic and where it sits.
  // Opens under the row on a tap or a click (a phone has no hover), and stays
  // open across a refresh.
  const andList = (xs) => xs.length < 2 ? xs.join("") : xs.slice(0, -1).join(", ") + " and " + xs[xs.length - 1];
  const NO_SERVICES_WHY = "This peer announced no services at all: it has nothing to offer other nodes";
  const NO_TX_WHY = "Wants no transactions from your node";
  const bytesText = (n) => n >= 1e9 ? (n / 1e9).toFixed(1) + "\u00a0GB" : n >= 1e6 ? Math.round(n / 1e6) + "\u00a0MB" : n >= 1e3 ? Math.round(n / 1e3) + "\u00a0kB" : n + "\u00a0B";
  function peerDetail(p) {
    const el = (tag, cls, text) => {
      const e = document.createElement(tag);
      if (cls) e.className = cls;
      if (text != null) e.textContent = text;
      return e;
    };
    const panel = el("div", "pd");
    const block = (title) => {
      const b = el("section", "pd-block");
      b.appendChild(el("h4", "", title));
      panel.appendChild(b);
      return b;
    };
    // One line of the connection: name on the left, value on the right, in
    // the size of the traffic table under it. These were tiles in 17px, the
    // largest type in the panel, and at 960px too narrow for "Inbound".
    const fact = (label, value) => {
      const s = el("div", "pd-fact");
      s.append(el("span", "", label), el("span", "", value));
      return s;
    };

    // What it is, and why each word in the row is there.
    const what = block("What it is");
    // Under a kind filter the row shows only part of it; the panel has all of it.
    if (p.about) {
      for (const line of p.about.split("\n")) {
        const m = /^(Claims|Probably|Also):\s*(.*)$/.exec(line);
        const row = el("p", "pd-about");
        if (m) {
          row.classList.add("tagged");
          // Further reasons stand under the first one, without a word of their own.
          row.append(el("span", "pd-tag " + m[1].toLowerCase(), m[1] === "Also" ? "" : m[1]), el("span", "", m[2]));
        }
        else row.textContent = line;
        what.appendChild(row);
      }
    }
    if (p.seen) what.appendChild(el("p", "pd-about", "In this session: " + p.seen));
    if (p.pool_proof) what.appendChild(el("p", "pd-about", p.pool_proof));
    const flags = [];
    // Each fact once: "never first" already says the peer offers no services or
    // is on a chain of its own, so those flags are folded into it, and "no tx"
    // stays out when the server says the line above already tells it.
    if (p.sync_mb) flags.push(["syncing", `Still syncing: it has taken ${mbText(p.sync_mb)} of blocks from this node`, "sync"]);
    if (p.fork) flags.push([p.fork, p.fork_why + (p.no_relay ? " - it can never deliver one of your blocks first." : "")]);
    else if (p.no_relay) flags.push(["never first", neverNote(p)]);
    if (p.no_services && !p.no_relay) flags.push(["no services", NO_SERVICES_WHY]);
    // A reason of its own (blocks-only connection, blocksonly=1) still stands.
    const plainNoTx = !p.no_tx_why || p.no_tx_why === NO_TX_WHY;
    if (p.no_tx_relay && !(p.no_tx_said && plainNoTx)) flags.push(["no tx", p.no_tx_why || NO_TX_WHY]);
    if (p.chain_unknown && !p.sync_mb) flags.push(["no headers", NO_HEADERS_WHY]);
    if (isPruned(p)) flags.push(["pruned", PRUNED_WHY, "", true]);
    // The fee rate below which it wants no transactions announced. A peer that
    // takes none at all sends the highest there is: "no tx" says that already.
    if (p.min_fee != null && p.min_fee < 1000)
      flags.push([(p.min_fee < 10 ? p.min_fee.toFixed(1).replace(/\.0$/, "") : Math.round(p.min_fee)) + " sat/B", "Min fee: cheaper transactions are not sent to it", "", true]);
    if (p.app) flags.push(["Umbrel app", p.app, "", true]);
    for (const [word, why, cls] of flags) {
      const row = el("p", "pd-flag-row");
      row.append(el("span", "pd-flag" + (cls ? " " + cls : ""), word), " " + why);
      // Without Bitcoin Lab next door nothing in the map says what "first"
      // is: where someone wonders, one line says where to find out.
      if (p.no_relay && (word === "never first" || word === p.fork) && !state.data.sibling) {
        const more = el("span", "pd-more", "Which of your peers bring you new blocks first: ");
        const a = el("a", "", "Bitcoin Cash Lab \u2197");
        a.href = LAB_PAGE;
        a.target = "_blank";
        a.rel = "noopener";
        more.appendChild(a);
        row.appendChild(more);
      }
      what.appendChild(row);
    }
    // Pruned, the fee filter and the Umbrel app only add context; they do not
    // take the honest line away.
    const onlyContext = !what.querySelector(".pd-about") && flags.every((f) => f[3]);
    if (!what.querySelector("p") || onlyContext) {
      if (p.kind.startsWith("Node")) {
        // A node with nothing to explain: say what is good about it, from what it did.
        // Not its services: they stand under Announced, and NETWORK_LIMITED in a
        // sentence read as pruned. Not "honest" either: nothing odd is all we know.
        const good = ["says what it is"];
        if (!p.no_tx_relay) good.push("relays transactions");
        if (!p.chain_unknown) good.push("shares its block headers");
        // Ten minutes is what the behaviour rules need before they speak: sooner, only "so far".
        const settled = p.conntime && Date.now() / 1000 - p.conntime >= 600;
        what.insertBefore(el("p", "pd-about", (settled ? "Nothing odd: it " : "Nothing odd so far: it ") + andList(good) + "."), what.querySelector("p"));
      } else if (!what.querySelector("p")) {
        // Nothing to say about the others either: no block rather than a made-up one.
        what.remove();
        panel.classList.add("pd-two");
      }
    }

    // The line: how it is connected, how fast, how long, how much each way.
    const line = block("Connection");
    const facts = el("div", "pd-facts");
    if (p.min_ping_ms != null) facts.appendChild(fact("Ping", fmtPing(p.min_ping_ms)));
    if (p.conntime) facts.appendChild(fact("Connected", ago(Date.now() / 1000 - p.conntime)));
    facts.appendChild(fact("Link", [LABEL[p.group], typeLabel(p.type).toLowerCase() === p.group ? "" : typeLabel(p.type),
      netLabel(p.network), p.transport].filter(Boolean).join(" \u00b7 ")));

    line.appendChild(facts);
    if (p.tor_exit) {
      const tor = el("p", "pd-flag-row");
      tor.append(el("span", "pd-flag", "Tor exit"), " " + TOR_EXIT_TITLE.replace(/^Tor exit: /, "Reached through a Tor exit: "));
      line.appendChild(tor);
    }
    const total = Math.max(1, p.bytes_sent + p.bytes_recv);
    const traffic = el("div", "pd-traffic");
    const bar = el("div", "pd-bar");
    const sent = el("span", "pd-sent"), got = el("span", "pd-got");
    sent.style.flexGrow = String(Math.max(p.bytes_sent / total, 0.02));
    got.style.flexGrow = String(Math.max(p.bytes_recv / total, 0.02));
    bar.append(sent, got);
    traffic.append(el("span", "pd-label", "Traffic"), bar,
      el("span", "pd-small", `\u2191 ${bytesText(p.bytes_sent)} sent to it \u00b7 \u2193 ${bytesText(p.bytes_recv)} from it`));
    if (p.traffic_sent || p.traffic_recv) {
      const breakdown = el("div", "pd-traffic-types");
      breakdown.setAttribute("role", "table");
      breakdown.setAttribute("aria-label", "Traffic by message type, since connection began");
      const headers = el("div", "pd-traffic-group");
      const header = el("div", "pd-traffic-row");
      header.setAttribute("role", "row");
      for (const label of ["Type", "To peer ↑", "From peer ↓"]) {
        const th = el("span", "pd-traffic-heading", label);
        th.setAttribute("role", "columnheader");
        header.appendChild(th);
      }
      headers.setAttribute("role", "rowgroup");
      headers.appendChild(header);
      breakdown.appendChild(headers);
      const body = el("div", "pd-traffic-group");
      for (const [key, label] of [["transactions", "Transactions"], ["blocks", "Blocks / headers"], ["addresses", "Addresses"], ["other", "Other messages"]]) {
        const row = el("div", "pd-traffic-row");
        row.setAttribute("role", "row");
        const name = el("span", "", label);
        name.setAttribute("role", "rowheader");
        row.appendChild(name);
        for (const counters of [p.traffic_sent, p.traffic_recv]) {
          const value = el("span", "", counters && counters[key] != null ? bytesText(counters[key]) : "–");
          value.setAttribute("role", "cell");
          row.appendChild(value);
        }
        body.appendChild(row);
      }
      body.setAttribute("role", "rowgroup");
      breakdown.appendChild(body);
      traffic.append(breakdown, el("span", "pd-small", "Bytes since this connection began."));
    }
    line.appendChild(traffic);

    // What it announced and where it sits.
    const who = block("Announced");
    who.appendChild(el("p", "pd-ua", p.subver || "no name sent"));
    const svc = el("div", "pd-svc");
    if (p.services && p.services.length) for (const s of p.services) svc.appendChild(el("span", "pd-chip", s));
    else svc.appendChild(el("span", "pd-chip none", p.no_services ? "no services" : "services unknown"));
    who.appendChild(svc);
    if (p.version) who.appendChild(el("p", "pd-small", "Protocol " + p.version));
    if (state.data && state.data.sibling) {
      const lab = el("a", "pd-lab", "Bitcoin Cash Lab \u2197");
      lab.title = "Open this peer in Bitcoin Cash Lab";
      lab.href = siblingURL(p.addr);
      lab.target = LAB_TARGET;
      who.appendChild(lab);
    }
    const where = [p.region, p.cc].filter(Boolean).join(", ");
    if (where || p.operator) who.appendChild(el("p", "pd-where", [where, p.operator ? p.operator + (p.asn ? " (AS" + p.asn + ")" : "") : ""].filter(Boolean).join(" \u00b7 ")));
    return panel;
  }
  // The detail is a row of its own under the peer, as wide as the table. It
  // adds no width to any column: its content is 0 wide and as wide as the cell.
  function detailRow(tr, p, key) {
    const row = document.createElement("tr");
    row.className = "detail-row";
    const td = document.createElement("td");
    td.colSpan = tr.cells.length;
    td.appendChild(peerDetail(p));
    row.appendChild(td);
    // A tap on the open panel closes it, so a phone need not go back up to
    // the row (04.11.2026). Links and buttons in it keep their own job. Its
    // row comes back into view if it had scrolled away above.
    row.addEventListener("click", (ev) => {
      if (ev.target.closest("a, button, input, select, textarea")) return;
      if (String(window.getSelection()).length > 0) return;
      // Its row on screen: the panel rolls up into it, in place. Scrolled
      // away: shut at once, and the row comes back to the top of the screen.
      const top = tr.getBoundingClientRect().top;
      if (top >= 0) toggleDetail(tr, p, key);
      else {
        setDetail(tr, p, key, false);
        window.scrollBy(0, tr.getBoundingClientRect().top - 8);
      }
      if (backY !== null) backY = window.scrollY;  // and the way back keeps this place
    });
    return row;
  }
  // One at a time: opening a peer closes the one that was open, so the list does
  // not grow without end. The row stays where it was on the screen, its panel
  // rolls out below it (foldInPlace), although a panel above it has just gone.
  function setDetail(tr, p, key, open) {
    for (const row of document.querySelectorAll("#tables tr.detail-row")) row.remove();
    for (const row of document.querySelectorAll("#tables tr.open")) {
      row.classList.remove("open");
      row.querySelector(".chev")?.setAttribute("aria-expanded", "false");
    }
    state.openRows.clear();
    if (open) {
      tr.after(detailRow(tr, p, key));
      tr.classList.add("open");
      tr.querySelector(".chev")?.setAttribute("aria-expanded", "true");
      state.openRows.add(key);
    }
    layers();
  }
  function toggleDetail(tr, p, key) {
    const open = !tr.nextElementSibling?.classList.contains("detail-row");
    foldInPlace({ anchor: tr, box: tr.closest(".card") || tr.closest("table"), open, set: (o) => setDetail(tr, p, key, o) });
  }

  // All inbound peers or the top ten. Opening, what is above stays and the
  // rest rolls out below it; closing from the button at the end, the button
  // stays under the finger and the list rolls up into it. From the bar at
  // the top there is no button there to keep: the list closes, and the
  // card's head comes back to the top of the screen.
  const inboundWrap = () => document.querySelector("#group-inbound .scroll");
  const moreButton = () => document.querySelector("#group-inbound > button.more");
  function showAllInbound(all) {
    foldInPlace({ anchor: all ? inboundWrap : moreButton, box: inboundWrap, open: all, set: (o) => {
      state.showAllInbound = o;
      renderTables();
    } });
  }
  function topOnly() {
    state.showAllInbound = false;
    renderTables();
    const c = $("group-inbound");
    if (c && c.getBoundingClientRect().top < 0) c.scrollIntoView({ block: "start", behavior: glide() });
  }
  function stickOn() {
    const stick = document.querySelector("#tables .stick");
    if (!stick) return;
    const card = stick.parentElement;
    stick.classList.toggle("on", card.querySelector(".group-head").getBoundingClientRect().bottom < 0
      && card.getBoundingClientRect().bottom > 140);
  }
  window.addEventListener("scroll", stickOn, { passive: true });

  function renderTables() {
    // A row reached with the arrow keys keeps the focus through a redraw, and
    // so does the button that redrew it (a sort, "Show all").
    const focused = document.activeElement && document.activeElement.dataset && document.activeElement.dataset.anchor;
    const focusKey = document.activeElement && document.activeElement.dataset && document.activeElement.dataset.focusKey;
    // A panel left open for a peer that is gone must not pop open when its address comes back.
    const live = new Set(state.data.peers.map((p) => p.group + " " + p.addr));
    for (const k of state.openRows) if (!live.has(k)) state.openRows.delete(k);
    const root = $("tables");
    root.textContent = "";
    root.removeAttribute("aria-busy");
    let picksShown = false;
    for (const g of GROUPS) {
      if (!state.show[g]) continue;
      let peers = state.data.peers.filter((p) => p.group === g);
      const all = peers.length;
      if (state.place) peers = peers.filter(peerMatchesPlace);
      const kindPool = peers;
      if (state.group) peers = peers.filter(matchesKind);

      const card = document.createElement("section");
      card.className = "card group-card";
      card.id = "group-" + g;
      const summary = document.createElement("div");
      summary.className = "group-head";
      const h = document.createElement("h2");
      const sw = document.createElement("span");
      sw.className = "swatch s-" + g;
      h.append(sw, LABEL[g]);
      foldable(card, h, "group-" + g);
      // The count and the mix of kinds as chips beside the name. Counted
      // before this group's own kind filter, so a group the filter emptied
      // still offers the way back - and switching kinds is one click.
      const mix = document.createElement("span");
      mix.className = "kinds chips";
      const n = document.createElement("span");
      n.className = "chip count";
      const ni = document.createElement("i");
      ni.textContent = state.place || state.group ? `${peers.length} of ${all}` : String(all);
      n.append(ni, " peers");
      mix.appendChild(n);
      // How many are catching up from this node right now, and what they took.
      const syncing = kindPool.filter((p) => p.sync_mb);
      if (syncing.length && (!state.group || state.group === SYNCING)) {
        const sc = document.createElement("button");
        sc.type = "button";
        sc.className = "chip kind-pick syncing" + (state.group === SYNCING ? " on" : "");
        const taken = `${syncing.length} syncing from this node, ${mbText(syncing.reduce((t, p) => t + p.sync_mb, 0))} of blocks taken so far`;
        sc.title = state.group === SYNCING ? "Show every kind again" : taken + " - click to show only these";
        sc.setAttribute("aria-pressed", String(state.group === SYNCING));
        const si = document.createElement("i");
        si.textContent = syncing.length;
        const ico = document.createElement("span");
        ico.className = "sync-ico";
        ico.setAttribute("aria-hidden", "true");
        ico.textContent = "\u21bb";
        sc.append(ico, si, " syncing");
        sc.addEventListener("click", () => steady(sc, () => setGroup(SYNCING)));
        mix.appendChild(sc);
      }
      const forked = kindPool.filter(offChain);
      if (forked.length && (!state.group || state.group === FORKED)) {
        const fc = document.createElement("button");
        fc.type = "button";
        fc.className = "chip kind-pick forked" + (state.group === FORKED ? " on" : "");
        fc.title = state.group === FORKED ? "Show every kind again"
          : `${forked.length} not on your chain (${[...new Set(forked.map((p) => p.fork || OTHER_CHAIN))].join(", ")}) - click to show only these`;
        fc.setAttribute("aria-pressed", String(state.group === FORKED));
        const fi = document.createElement("i");
        fi.textContent = forked.length;
        fc.append(fi, " other chain");
        fc.addEventListener("click", () => steady(fc, () => setGroup(FORKED)));
        mix.appendChild(fc);
      }
      if (kindPool.length) renderKindMix(mix, kindPool);
      summary.append(h, mix);
      card.appendChild(summary);
      if (state.group && !special(state.group)) {
        const inGroup = kindPool.filter((p) => groupOf(p.kind) === state.group);
        renderKindPicks(card, inGroup);
        // The names belong to one kind: the one picked, or the only one there is.
        const kinds = new Set(inGroup.map((p) => p.kind));
        const kind = state.kind || (kinds.size === 1 ? [...kinds][0] : null);
        if (kind) {
          picksShown = true;
          renderSubMix(card, kind, inGroup.filter((p) => p.kind === kind));
          renderLabelMix(card, inGroup.filter((p) => p.kind === kind));
        }
      }

      if (peers.length) {
        const spread = document.createElement("p");
        spread.className = "kinds spread";
        spread.textContent = spreadText(peers);
        card.appendChild(spread);
      }

      if (!peers.length) {
        const p = document.createElement("p");
        p.className = "empty";
        // No inbound at all is what most nodes see; say what brings them.
        p.textContent = state.place || state.group
          ? "None here."
          : g === "inbound" ? "No inbound peers. For incoming clearnet connections, allow incoming clearnet in your Bitcoin Node app and open port 8333 on your router."
          : "No " + LABEL[g].toLowerCase() + " peers.";
        card.appendChild(p);
        root.appendChild(card);
        continue;
      }

      const cols = COLS;
      const s = state.sort[g] || { key: "conntime", dir: 1 };
      const col = cols.find((c) => c.key === s.key) || cols[cols.length - 1];
      peers.sort((a, b) => {
        const x = col.val(a), y = col.val(b);
        return (x < y ? -1 : x > y ? 1 : 0) * s.dir;
      });

      const wrap = document.createElement("div");
      wrap.className = "scroll";
      const table = document.createElement("table");
      table.className = "peers";
      const thead = document.createElement("thead");
      const hr = document.createElement("tr");
      for (const c of cols) {
        const th = document.createElement("th");
        th.scope = "col";
        th.className = "k-" + c.key + (c.cls ? " " + c.cls : "");
        if (c.key === col.key) th.setAttribute("aria-sort", s.dir > 0 ? "ascending" : "descending");
        const b = document.createElement("button");
        b.textContent = c.label;
        b.dataset.focusKey = "sort " + g + " " + c.key;
        b.addEventListener("click", () => steady(b, () => {
          state.sort[g] = { key: c.key, dir: c.key === col.key ? -s.dir : 1 };
          renderTables();
        }));
        th.appendChild(b);
        hr.appendChild(th);
      }
      thead.appendChild(hr);
      table.appendChild(thead);
      // Inbound runs to a couple of hundred rows and pushed Outbound and Manual
      // far down the page, so it shows its top ten - in the order of the
      // current sort - with a toggle for the rest, as Bitcoin Lab's ranking
      // does. The others never get long. A peer Bitcoin Lab pointed at opens
      // the full list once, or its row would not exist to land on.
      let shown = peers;
      const limited = g === "inbound" && peers.length > INBOUND_LIMIT;
      if (limited) {
        if (state.focus && !state.focusDone && !state.showAllInbound
            && peers.slice(INBOUND_LIMIT).some((p) => p.addr === state.focus)) state.showAllInbound = true;
        if (!state.showAllInbound) shown = peers.slice(0, INBOUND_LIMIT);
      }
      const tbody = document.createElement("tbody");
      for (const p of shown) {
        const tr = document.createElement("tr");
        tr.dataset.anchor = g + " " + p.addr;
        if (state.focus && p.addr === state.focus) tr.className = "focus";
        // The peer that brought the newest block, for as long as the star
        // is up - one rule for "just delivered", in the row and on the map.
        else if (deliveredNow.has(p.addr)) {
          tr.className = "delivered-now";
        }
        // A connection that has brought this node a block, at least once: its address in green, also on an orange or
        // focused row (as in Bitcoin Lab).
        if (deliveredEver.has(p.addr) || deliveredNow.has(p.addr)) tr.classList.add("delivered-ever");
        if (p.no_relay) tr.classList.add("weak");
        for (const c of cols) {
          const td = document.createElement("td");
          if (c.cls) td.className = c.cls;
          td.classList.add("k-" + c.key);
          td.dataset.label = c.label;
          if (c.cell) {
            c.cell(td, p);
          } else {
            const text = c.show ? c.show(p) : c.val(p);
            td.textContent = text === "" ? "–" : text;
            if (text === "") td.classList.add("dim");
          }
          tr.appendChild(td);
        }
        const rowKey = g + " " + p.addr;
        if (arrivedAt.has(rowKey)) tr.classList.add("arrived");
        const reopen = state.openRows.has(rowKey);
        if (reopen) {
          tr.classList.add("open");
          tr.querySelector(".chev")?.setAttribute("aria-expanded", "true");
        }
        // With a mouse, the row's place lights up on the map.
        tr.addEventListener("mouseenter", () => hoverPlace(p, true));
        tr.addEventListener("mouseleave", () => hoverPlace(p, false));
        tr.tabIndex = -1;
        tr.addEventListener("click", (ev) => {
          // Links and buttons keep doing their own thing; so does selecting text.
          // On a touch screen the address is too small a target for a finger: a tap
          // on it opens the peer like a tap anywhere else, and the way over to
          // Bitcoin Lab is a button in the panel.
          const onAddress = ev.target.closest("a.peer-jump") && window.matchMedia("(pointer: coarse)").matches;
          if (onAddress) ev.preventDefault();
          else if (ev.target.closest("a, button, input, select, textarea") && !ev.target.closest(".chev")) return;
          if (String(window.getSelection()).length > 0) return;
          toggleDetail(tr, p, rowKey);
        });
        tbody.appendChild(tr);
        if (reopen) tbody.appendChild(detailRow(tr, p, rowKey));
      }
      table.appendChild(tbody);
      wrap.appendChild(table);
      card.appendChild(wrap);
      if (limited) {
        const more = document.createElement("button");
        more.type = "button";
        more.className = "more";
        more.dataset.focusKey = "more " + g;
        more.textContent = state.showAllInbound ? `Show top ${INBOUND_LIMIT} only` : `Show all ${peers.length}`;
        more.addEventListener("click", () => showAllInbound(!state.showAllInbound));
        card.appendChild(more);
        // The whole list open: its name and the way back to the top ten ride
        // along at the top of the screen once the head has scrolled away, so
        // nobody has to scroll to the end to close it.
        if (state.showAllInbound) {
          const stick = document.createElement("div");
          stick.className = "stick";
          const bar = document.createElement("div");
          const name = document.createElement("span");
          const ssw = document.createElement("span");
          ssw.className = "swatch s-" + g;
          name.append(ssw, `${LABEL[g]} · ${peers.length}`);
          const back = document.createElement("button");
          back.type = "button";
          back.className = "btn";
          back.textContent = `Top ${INBOUND_LIMIT}`;
          back.addEventListener("click", topOnly);
          bar.append(name, back);
          stick.appendChild(bar);
          summary.after(stick);
        }
      }
      root.appendChild(card);
    }
    // A second kind turned up since a name was picked: no card shows the name
    // chips any more, so the pick goes too rather than filter two kinds unseen.
    // Asked of all cards together: the pick sits in one of them, and asked of
    // each card, any other card without its chips dropped every pick the
    // moment it was made (2.5.0).
    if (state.group && !special(state.group) && !picksShown && (state.sub || state.label)) {
      state.sub = state.label = null;
      queueMicrotask(applyFilters);
    }
    for (const row of root.querySelectorAll(".one-line")) fitOneLine(row);
    fitTables();
    stickOn();
    if (focused) {
      const back = root.querySelector(`tr[data-anchor="${CSS.escape(focused)}"]`);
      if (back) back.focus({ preventScroll: true });
    } else if (focusKey) {
      const back = root.querySelector(`[data-focus-key="${CSS.escape(focusKey)}"]`);
      if (back) back.focus({ preventScroll: true });
    }
  }

  // A table that does not fit its card turns each row into a small card of
  // its own (see .scroll.cards in style.css) instead of scrolling sideways:
  // every value stays, below each other rather than beside. Decided by
  // measuring, not by a fixed screen width - the columns are as wide as
  // their content, and a closed group has no width to measure until it opens.
  // All groups switch together: one group as cards and the next as a table
  // looked like two different pages.
  // Measuring takes the cards away for a moment, and on a phone the page is
  // then far shorter: Safari clamps the scroll position to it and keeps it
  // there, so a reader far down was thrown up on every refresh. The position
  // is put back before anything is painted.
  //
  // Before that, a tight table gives up what matters least, a step at a time
  // (see [data-drops] in style.css): Connected, then Ping, then Net, and
  // Operator last (Ping and Connected matter less than anything
  // else). The first three steps are also taken when the tables would fit
  // but the kind's text would be cut. The address always stays: it is the
  // link into Bitcoin Lab.
  const DROP_LEVELS = 4, KIND_LEVEL = 3;
  function fitTables() {
    const y = window.scrollY;
    const wraps = [...document.querySelectorAll("#tables .scroll")];
    // A second reason never decides which columns go: measured with the first
    // alone, and put back below where it fits whole.
    const seconds = [...document.querySelectorAll("#tables [data-one]")];
    for (const e of seconds) e.textContent = e.dataset.one;
    const unalign = () => { for (const w of wraps) w.querySelector("table.peers")?.classList.remove("aligned"); };
    for (const w of wraps) w.classList.remove("cards");
    // Tight when the shared widths (alignColumns) together are wider than
    // the card. Each table is measured there too; asking whether one table
    // overflows on its own counted Kind at its full length again.
    const tight = () => {
      unalign();
      return sharedTooWide(wraps);
    };
    let level = 0;
    for (; ; level++) {
      for (const w of wraps) w.dataset.drops = String(level);
      if (level === DROP_LEVELS) break;
      if (tight()) continue;
      // The sum above is a forecast. What the laid-out table really needs
      // decides: a forecast a few pixels short let Crawler and Core 22.x
      // run 2-3px past the card at 960px.
      alignColumns(wraps);
      if (overflows(wraps)) continue;
      if (level < KIND_LEVEL && kindCut(wraps)) continue;
      break;
    }
    let cards = level === DROP_LEVELS && tight();
    if (!cards) {
      alignColumns(wraps);
      cards = level === DROP_LEVELS && overflows(wraps);
      if (cards) unalign();
    }
    for (const w of wraps) w.classList.toggle("cards", cards);
    // A second reason only where it fits whole, never cut short (09.11.2026):
    // on a card its own line, in the wide list the grey line it sits in.
    for (const e of seconds) {
      const box = e.closest("small") || e;
      e.textContent = e.dataset.full;
      if (box.scrollWidth > box.clientWidth + 1) e.textContent = e.dataset.one;
    }
    // A phone puts the kind beside the place. Reserve its actual width,
    // including either or both relay marks, instead of a fixed pill-sized gap.
    for (const w of wraps) for (const row of w.querySelectorAll("tr[data-anchor]")) {
      const head = row.querySelector(".kind-head");
      if (cards && head) row.style.setProperty("--kind-space", Math.ceil(head.getBoundingClientRect().width) + 12 + "px");
      else row.style.removeProperty("--kind-space");
    }
    if (window.scrollY !== y) window.scrollTo(0, y);
  }
  const overflows = (wraps) => wraps.some((w) => w.clientWidth && w.scrollWidth > w.clientWidth + 1);
  const kindCut = (wraps) => wraps.some((w) =>
    [...w.querySelectorAll("td.k-kind small.ua")].some((e) => e.offsetWidth && e.scrollWidth > e.clientWidth + 1));

  // Inbound, Outbound and Manual have the same columns, and each table sized
  // them to its own content: Operator, Kind, Net and Ping stood in a
  // different place in every group, so the page read as three. Each column
  // now gets the widest of the three natural widths, the same in every
  // table, and fixed layout spreads the rest of the card's width the same way
  // in each. A closed group has no width to measure and is left out until it
  // opens (the toggle listener runs this again).
  const colKey = (th) => [...th.classList].find((c) => c.startsWith("k-")) || String(th.cellIndex);
  function sharedWidths(wraps) {
    const tables = wraps.map((w) => w.querySelector("table.peers")).filter((t) => t && t.offsetWidth);
    const widths = new Map();
    for (const t of tables) {
      t.classList.add("measure");
      for (const th of t.tHead.rows[0].cells) th.style.width = "";
    }
    for (const t of tables) {
      for (const th of t.tHead.rows[0].cells) {
        widths.set(colKey(th), Math.max(widths.get(colKey(th)) || 0, th.getBoundingClientRect().width));
      }
      t.classList.remove("measure");
    }
    return { tables, widths };
  }
  // For the question whether columns must go, Kind counts as wide as an
  // ordinary entry needs (a pill and a line of what Core saw): one long text
  // among two hundred peers took Operator, Net, Ping and Connected away from
  // all three tables and left Outbound and Manual mostly empty. A longer
  // text is cut and shown whole on hover.
  const KIND_MIN = 200;
  function sharedTooWide(wraps) {
    const { tables, widths } = sharedWidths(wraps);
    let sum = 0;
    // alignColumns never gives Kind less than KIND_MIN, so that is what it
    // counts here too: a short kind (a filter showing only the reason line)
    // passed as fitting and then pushed the table past the card's edge.
    // The others are rounded up the way alignColumns rounds them.
    for (const [k, v] of widths) sum += k === "k-kind" ? KIND_MIN : Math.ceil(v);
    return tables.some((t) => sum > t.parentElement.clientWidth + 1);
  }
  // Kind gets what it needs - cut, and whole on hover, only when there is
  // truly no room - and what the card has left over is shared by every
  // column in proportion to its width. Kind used to take all of it, which was
  // right while it carried the peer's own name; without that it stood half
  // empty across a wide screen (28.10.2026).
  function alignColumns(wraps) {
    const { tables, widths } = sharedWidths(wraps);
    let others = 0;
    for (const [k, v] of widths) if (k !== "k-kind") others += Math.ceil(v);
    for (const t of tables) {
      const spare = t.parentElement.clientWidth - others;
      const kind = Math.max(KIND_MIN, Math.min(spare, Math.ceil(widths.get("k-kind") || 0)));
      const total = others + kind;
      const grow = spare > kind ? (t.parentElement.clientWidth) / total : 1;
      let used = 0;
      const cells = [...t.tHead.rows[0].cells];
      cells.forEach((th, i) => {
        const k = colKey(th);
        const base = k === "k-kind" ? (spare > kind ? kind : Math.max(KIND_MIN, spare)) : Math.ceil(widths.get(k));
        // The last column takes the rounding, so the row ends at the card's edge.
        const w = i === cells.length - 1 && grow > 1 ? t.parentElement.clientWidth - used : Math.floor(base * grow);
        used += w;
        th.style.width = w + "px";
      });
      t.classList.add("aligned");
    }
  }
  let fitTimer = null;
  // Only a change of width can change whether the tables fit; on an iPhone the
  // height changes all the time while Safari's toolbar shrinks and grows.
  let fitWidth = window.innerWidth;
  window.addEventListener("resize", () => {
    if (window.innerWidth === fitWidth) return;
    fitWidth = window.innerWidth;
    clearTimeout(fitTimer); fitTimer = setTimeout(fitTables, 100);
  });
  document.addEventListener("toggle", fitTables, true);


  // ---------- start ----------
  setupPanZoom();
  loadWorld()
    .then(() => { if (state.data) renderMarkers(); })
    .catch((e) => { state.worldError = "The world map could not be loaded: " + e.message; showError(state.worldError); });
  startTicker();
  refresh();
})();
