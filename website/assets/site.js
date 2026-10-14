/* Shared by index.html, zh-hant.html and en.html. Everything here is an
   enhancement: without it the pages keep their content, the language
   links still work, and the demo shows the finished sentence. */
(function () {
  "use strict";

  /* A language link keeps the section being read. */
  var lang = document.getElementById("lang");
  if (lang) lang.addEventListener("click", function (e) {
    var a = e.target.closest && e.target.closest("a[href]");
    if (!a) return;
    var id = "";
    document.querySelectorAll("main section[id]").forEach(function (s) {
      if (s.getBoundingClientRect().top <= 140) id = s.id;
    });
    var file = a.getAttribute("href").split("#")[0];
    a.setAttribute("href", id && id !== "top" ? file + "#" + id : file);
  });

  /* The dictation demo. A simulation with the page's preset text: it never
     asks for the microphone. One run is: listening (the waveform moves and
     the words appear in the floating bar as they are heard), a short
     processing state, then the text typed at the caret.
     - It plays once by itself when it scrolls into view.
     - The Fn key starts a run, and pressed while listening, ends listening.
     - Replay starts over; Pause holds a run where it is.
     - With reduced motion nothing plays by itself and text is not typed
       letter by letter; the states still follow one another on request. */
  var $ = function (id) { return document.getElementById(id); };
  var stage = $("stage"), key = $("key"), hint = $("hint"), typed = $("typed");
  var pill = $("pill-text"), live = $("live"), tidy = $("tidy");
  var transport = $("transport"), replay = $("replay"), pause = $("pause");
  if (!stage || !key || !hint || !typed || !pill || !live || !tidy || !transport || !replay || !pause) return;

  var S = stage.dataset;
  var still = window.matchMedia("(prefers-reduced-motion: reduce)").matches;
  var timer = 0, pending = null, due = 0, left = 0, paused = false;

  /* One step waits at a time, so a pause only has to hold that one. */
  function wait(ms, fn) {
    pending = fn;
    if (paused) { left = ms; return; }
    due = Date.now() + ms;
    timer = setTimeout(function () { pending = null; fn(); }, ms);
  }
  function stop() { clearTimeout(timer); pending = null; }
  function setPaused(on) {
    paused = on;
    if (on) { S.paused = ""; if (pending) { clearTimeout(timer); left = Math.max(0, due - Date.now()); } }
    else { delete S.paused; if (pending) wait(left, pending); }
    pause.setAttribute("aria-pressed", on ? "true" : "false");
    pause.lastElementChild.textContent = on ? S.resume : S.pause;
  }
  function phase(name, text) {
    S.phase = name;
    hint.textContent = text;
    pause.disabled = name === "idle";
  }
  function sentence() { return tidy.checked ? S.clean : S.raw; }
  function pace(text, total, lo, hi) { return Math.max(lo, Math.min(hi, Math.round(total / text.length))); }

  /* Shows text in el a few characters at a time, then calls then. */
  function stream(el, text, ms, then) {
    if (still) { el.textContent = text; then(); return; }
    var i = 0;
    (function step() {
      el.textContent = text.slice(0, ++i);
      if (i < text.length) wait(ms, step); else then();
    })();
  }

  function listen(byHand) {
    stop(); setPaused(false);
    typed.textContent = ""; live.textContent = "";
    pill.textContent = S.pillListen;
    phase("listening", S.listening);
    wait(still ? 0 : 500, function () {
      stream(live, S.raw, pace(S.raw, 2600, 22, 90), function () {
        // Played by itself, listening ends by itself; started with the
        // key, it waits for the key (or ends after a while).
        wait(byHand ? 6000 : still ? 1200 : 700, work);
      });
    });
  }
  function work() {
    stop();
    pill.textContent = tidy.checked ? S.pillTidy : S.pillWork;
    phase("working", tidy.checked ? S.workTidy : S.work);
    wait(still ? 900 : 1100, insert);
  }
  function insert() {
    var text = sentence();
    phase("inserting", tidy.checked ? S.workTidy : S.work);
    stream(typed, text, pace(text, 800, 10, 34), done);
  }
  function done() {
    stop(); live.textContent = "";
    phase("idle", tidy.checked ? S.doneTidy : S.done);
  }

  key.addEventListener("click", function () {
    if (paused) setPaused(false);
    if (S.phase === "idle") listen(true);
    else if (S.phase === "listening") work();
  });
  replay.addEventListener("click", function () { listen(false); });
  pause.addEventListener("click", function () { if (S.phase !== "idle") setPaused(!paused); });
  tidy.addEventListener("change", function () {
    if (S.phase === "idle" && typed.textContent) { typed.textContent = sentence(); done(); }
  });

  transport.hidden = false;
  if (still || !("IntersectionObserver" in window)) return; // the finished sentence stays; Replay or Fn plays it
  typed.textContent = "";
  var seen = new IntersectionObserver(function (entries) {
    if (!entries.some(function (e) { return e.isIntersecting; })) return;
    seen.disconnect();
    if (S.phase === "idle" && !typed.textContent) listen(false);
  }, { threshold: 0.6 });
  seen.observe(stage);
})();
