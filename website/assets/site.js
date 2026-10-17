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

  /* The header is transparent with light text while it sits on the hero's
     dark sky, and turns solid once the page scrolls past it. */
  var head = document.getElementById("site-head"), hero = document.getElementById("top");
  if (head && hero) {
    var sky = function () {
      if (hero.querySelector("h1").getBoundingClientRect().bottom > head.offsetHeight) head.dataset.over = "hero";
      else delete head.dataset.over;
    };
    sky();
    window.addEventListener("scroll", sky, { passive: true });
    window.addEventListener("resize", sky);
  }

  /* Native, reversible scroll narrative. No timers or pointer effects. */
  (function () {
    var root=document.documentElement, mq=matchMedia('(prefers-reduced-motion: reduce)'), narrow=matchMedia('(max-width: 760px)');
    var story=document.querySelector('.product-story'), copy=document.querySelector('.hero-copy'), panels=Array.from(document.querySelectorAll('.story-panel')), bars=Array.from(document.querySelectorAll('.story-progress i'));
    if(!story) return;
    var raf=0, active=false;
    function clamp(n){return Math.max(0,Math.min(1,n));}
    function smooth(n){n=clamp(n);return n*n*(3-2*n);}
    function render(){
      raf=0;if(!active)return;
      var r=story.getBoundingClientRect(), vh=innerHeight;
      var entry=clamp((vh-r.top)/(vh-90));
      var p=clamp((90-r.top)/Math.max(1,story.offsetHeight-(vh-90)));
      var a=smooth((p-.28)/.12),b=smooth((p-.64)/.12),values=[1-a,a*(1-b),b];
      panels.forEach(function(el,i){
        el.style.opacity=values[i];el.style.visibility=values[i]>.001?'visible':'hidden';el.setAttribute('aria-hidden',values[i]<.5?'true':'false');
        var scale=i===0?.84+.16*smooth(entry):.94+.06*smooth(i===1?(p-.28)/.28:(p-.64)/.3);
        el.querySelector('.story-image').style.transform='perspective(1600px) rotateX('+ (i===0?(1-entry)*10:0)+'deg) scale('+scale+')';
        var caption=el.querySelector('figcaption');caption.style.opacity=clamp((values[i]-.5)*2);
        bars[i].style.opacity=values[i]>.5?'1':'.2';
      });
      var fade=clamp(scrollY/(vh*.52));copy.style.transform='translateY('+(-fade*85)+'px)';copy.style.opacity=1-fade;
    }
    function queue(){if(active&&!raf)raf=requestAnimationFrame(render);}
    function sync(){
      active=!mq.matches&&!narrow.matches;root.classList.toggle('scroll-story',active);
      if(active)queue();else{
        cancelAnimationFrame(raf);raf=0;copy.style.transform='';copy.style.opacity='';
        panels.forEach(function(el){el.style.opacity='';el.style.visibility='';el.removeAttribute('aria-hidden');el.querySelector('.story-image').style.transform='';el.querySelector('figcaption').style.opacity='';});
      }
    }
    addEventListener('scroll',queue,{passive:true});addEventListener('resize',queue);mq.addEventListener('change',sync);narrow.addEventListener('change',sync);sync();
  })();

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
  var still = false;
  (function (mq) {
    still = mq.matches;
    var update = function () { still = mq.matches; };
    if (mq.addEventListener) mq.addEventListener("change", update); else if (mq.addListener) mq.addListener(update);
  })(window.matchMedia("(prefers-reduced-motion: reduce)"));
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
})();
