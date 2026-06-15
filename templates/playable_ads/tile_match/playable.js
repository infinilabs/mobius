(function () {
  var ad = document.getElementById("ad");
  var board = document.getElementById("board");
  var rack = document.getElementById("rack");
  var goalText = document.getElementById("goalText");
  var progressFill = document.getElementById("progressFill");
  var timerText = document.getElementById("timerText");
  var hand = document.getElementById("hintHand");
  var overlay = document.getElementById("winOverlay");
  var endTitle = document.getElementById("endTitle");
  var endCopy = document.getElementById("endCopy");
  var cta = document.getElementById("ctaButton");
  var mainCta = document.getElementById("mainCtaButton");
  var canvas = document.getElementById("fxCanvas");
  var ctx = canvas.getContext("2d");

  var animals = {
    panda: { label: "PANDA", icon: "assets/playable/icon-panda.png", color: "#1d5f4f" },
    fox: { label: "FOX", icon: "assets/playable/icon-fox.png", color: "#d45b16" },
    rabbit: { label: "RABBIT", icon: "assets/playable/icon-rabbit.png", color: "#c53226" },
    cat: { label: "CAT", icon: "assets/playable/icon-cat.png", color: "#c52818" },
    turtle: { label: "TURTLE", icon: "assets/playable/icon-turtle.png", color: "#2f7f22" },
    bird: { label: "BIRD", icon: "assets/playable/icon-bird.png", color: "#176eb5" },
    fish: { label: "FISH", icon: "assets/playable/icon-fish.png", color: "#28231d" },
    frog: { label: "FROG", icon: "assets/playable/icon-frog.png", color: "#77a781" }
  };

  var layout = [
    { id: "panda", x: 18, y: 6, z: 18, r: -1, s: .98, d: 0 },
    { id: "fox", x: 35, y: 5, z: 24, r: 1, s: 1, d: 1 },
    { id: "rabbit", x: 52, y: 6, z: 22, r: -1, s: 1, d: 2 },
    { id: "cat", x: 69, y: 8, z: 17, r: 1, s: .98, d: 3 },

    { id: "turtle", x: 8, y: 22, z: 34, r: -1, s: .99, d: 4 },
    { id: "bird", x: 25, y: 20, z: 48, r: 1, s: 1.03, d: 5 },
    { id: "panda", x: 42, y: 22, z: 54, r: 0, s: 1.04, d: 6 },
    { id: "turtle", x: 59, y: 20, z: 47, r: -1, s: 1.02, d: 7 },
    { id: "fish", x: 76, y: 23, z: 33, r: 1, s: .99, d: 8 },

    { id: "fish", x: 1, y: 39, z: 58, r: -1, s: 1, d: 9 },
    { id: "cat", x: 18, y: 37, z: 76, r: 1, s: 1.04, d: 10 },
    { id: "rabbit", x: 35, y: 36, z: 88, r: 0, s: 1.07, d: 11 },
    { id: "frog", x: 52, y: 38, z: 82, r: -1, s: 1.05, d: 12 },
    { id: "bird", x: 69, y: 37, z: 74, r: 1, s: 1.04, d: 13 },
    { id: "turtle", x: 84, y: 40, z: 56, r: -1, s: .98, d: 14 },

    { id: "bird", x: 8, y: 57, z: 96, r: 1, s: 1.03, d: 15 },
    { id: "frog", x: 25, y: 55, z: 112, r: -1, s: 1.06, d: 16 },
    { id: "rabbit", x: 42, y: 55, z: 124, r: 0, s: 1.08, d: 17 },
    { id: "fox", x: 59, y: 55, z: 111, r: 1, s: 1.06, d: 18 },
    { id: "panda", x: 76, y: 57, z: 94, r: -1, s: 1.03, d: 19 },

    { id: "cat", x: 18, y: 73, z: 132, r: -1, s: 1.03, d: 20 },
    { id: "frog", x: 35, y: 72, z: 146, r: 1, s: 1.06, d: 21 },
    { id: "fox", x: 52, y: 72, z: 144, r: 0, s: 1.06, d: 22 },
    { id: "fish", x: 69, y: 73, z: 130, r: 1, s: 1.03, d: 23 }
  ];

  var rackItems = [];
  var clearedTriples = 0;
  var targetTriples = 8;
  var locked = false;
  var audioCtx = null;
  var hintTimer = null;
  var hintShown = false;
  var timeLeft = 60;
  var timerId = null;
  var particles = [];
  var raf = 0;

  function resizeCanvas() {
    var ratio = window.devicePixelRatio || 1;
    canvas.width = Math.floor(window.innerWidth * ratio);
    canvas.height = Math.floor(window.innerHeight * ratio);
    canvas.style.width = window.innerWidth + "px";
    canvas.style.height = window.innerHeight + "px";
    ctx.setTransform(ratio, 0, 0, ratio, 0, 0);
  }

  function getAudio() {
    if (!audioCtx) {
      var AudioContext = window.AudioContext || window.webkitAudioContext;
      if (AudioContext) audioCtx = new AudioContext();
    }
    if (audioCtx && audioCtx.state === "suspended") audioCtx.resume();
    return audioCtx;
  }

  function tone(freq, duration, type, volume, delay) {
    var actx = getAudio();
    if (!actx) return;
    var start = actx.currentTime + (delay || 0);
    var osc = actx.createOscillator();
    var gain = actx.createGain();
    osc.type = type || "sine";
    osc.frequency.setValueAtTime(freq, start);
    gain.gain.setValueAtTime(0.0001, start);
    gain.gain.exponentialRampToValueAtTime(volume || 0.055, start + 0.018);
    gain.gain.exponentialRampToValueAtTime(0.0001, start + duration);
    osc.connect(gain);
    gain.connect(actx.destination);
    osc.start(start);
    osc.stop(start + duration + 0.03);
  }

  function playPick() {
    tone(520, .07, "triangle", .035);
    tone(720, .08, "sine", .024, .045);
  }

  function playTriple() {
    tone(660, .11, "triangle", .055);
    tone(920, .13, "triangle", .05, .075);
    tone(1260, .17, "sine", .038, .15);
  }

  function playLose() {
    tone(170, .2, "sawtooth", .035);
    tone(120, .24, "sawtooth", .025, .12);
  }

  function playWin() {
    [523, 659, 784, 1046, 1318].forEach(function (freq, index) {
      tone(freq, .22, "triangle", .055, index * .085);
    });
  }

  function buildBoard() {
    board.innerHTML = "";
    layout.forEach(function (item, index) {
      var data = animals[item.id];
      var tile = document.createElement("button");
      tile.type = "button";
      tile.className = "tile animal-" + item.id;
      tile.dataset.id = item.id;
      tile.dataset.icon = data.icon;
      tile.style.setProperty("--icon", "url('" + data.icon + "')");
      tile.style.setProperty("--c", data.color);
      tile.style.setProperty("--s", item.s);
      tile.style.setProperty("--r", item.r + "deg");
      tile.style.animationDelay = item.d * .04 + "s";
      tile.setAttribute("aria-label", data.label);
      tile.innerHTML = '<span class="tile-face"><span class="tile-icon"></span></span>';
      tile.addEventListener("pointerdown", onTile);
      board.appendChild(tile);
      setTilePosition(tile, item);
      tile.style.zIndex = String(100 + Math.round(item.z));
    });
    updateRack();
    updateCapacity();
    startTimer();
    scheduleOpeningHint();
  }

  function setTilePosition(tile, item) {
    var boardRect = board.getBoundingClientRect();
    var tileW = Math.max(58, Math.min(84, boardRect.width * .185));
    var tileH = tileW * 1.255;
    var x = boardRect.width * item.x / 100;
    var y = boardRect.height * item.y / 100;
    tile.style.setProperty("--tile-w", tileW + "px");
    tile.style.setProperty("--tile-h", tileH + "px");
    tile.style.setProperty("--x", x + "px");
    tile.style.setProperty("--y", y + "px");
    tile.style.setProperty("--z", item.z + "px");
  }

  function refreshTilePositions() {
    Array.prototype.slice.call(board.children).forEach(function (tile, index) {
      setTilePosition(tile, layout[index]);
    });
  }

  function onTile(event) {
    if (locked) return;
    getAudio();
    clearHint();

    var tile = event.currentTarget;
    if (tile.classList.contains("disabled") || tile.classList.contains("cleared")) return;

    locked = true;
    tile.classList.add("disabled", "matched");
    flyTileToRack(tile, rackItems.length);
    rackItems.push({
      id: tile.dataset.id,
      icon: tile.dataset.icon,
      tile: tile
    });
    updateCapacity();
    playPick();

    window.setTimeout(function () {
      tile.classList.add("cleared");
      updateRack();
      if (!resolveRack()) {
        locked = false;
      }
    }, 540);
  }

  function resolveRack() {
    var tripleId = findTriple();
    if (tripleId) {
      locked = true;
      window.setTimeout(function () {
        clearTriple(tripleId);
      }, 120);
      return true;
    }
    if (rackItems.length >= 7) {
      showEnd(false);
      return true;
    }
    if (board.querySelectorAll(".tile:not(.cleared)").length === 0 || clearedTriples >= targetTriples) {
      showEnd(true);
      return true;
    }
    return false;
  }

  function findTriple() {
    var counts = {};
    for (var i = 0; i < rackItems.length; i += 1) {
      counts[rackItems[i].id] = (counts[rackItems[i].id] || 0) + 1;
      if (counts[rackItems[i].id] >= 3) return rackItems[i].id;
    }
    return "";
  }

  function clearTriple(id) {
    var removed = [];
    rackItems = rackItems.filter(function (item) {
      if (item.id === id && removed.length < 3) {
        removed.push(item);
        return false;
      }
      return true;
    });

    removed.forEach(function (item, index) {
      var slot = rack.querySelectorAll(".slot")[index];
      burstAtSlot(slot || rack, 16);
      if (item.tile) item.tile.remove();
    });

    clearedTriples += 1;
    playTriple();
    ad.classList.add("shake");
    window.setTimeout(function () {
      ad.classList.remove("shake");
      updateRack();
      updateCapacity();
      locked = false;
      if (board.querySelectorAll(".tile:not(.cleared)").length === 0 || clearedTriples >= targetTriples) {
        showEnd(true);
      }
    }, 320);
  }

  function updateRack() {
    var slots = rack.querySelectorAll(".slot");
    slots.forEach(function (slot, index) {
      var item = rackItems[index];
      slot.classList.toggle("filled", !!item);
      if (item) {
        slot.style.setProperty("--slot-icon", "url('" + item.icon + "')");
        slot.innerHTML = '<span class="slot-tile"><span class="slot-symbol"></span></span>';
      } else {
        slot.style.removeProperty("--slot-icon");
        slot.innerHTML = "";
      }
    });
  }

  function updateCapacity() {
    goalText.textContent = rackItems.length + "/7";
    progressFill.style.width = Math.min(rackItems.length / 7 * 100, 100) + "%";
  }

  function flyTileToRack(tile, slotIndex) {
    var tileRect = tile.getBoundingClientRect();
    var slots = rack.querySelectorAll(".slot");
    var target = slots[Math.max(0, Math.min(slots.length - 1, slotIndex))].getBoundingClientRect();
    var dx = target.left + target.width / 2 - (tileRect.left + tileRect.width / 2);
    var dy = target.top + target.height / 2 - (tileRect.top + tileRect.height / 2);
    var scale = Math.max(.58, Math.min(.78, target.width / tileRect.width * 1.04));
    tile.style.setProperty("--fly-x", "calc(var(--x) + " + dx + "px)");
    tile.style.setProperty("--fly-y", "calc(var(--y) + " + dy + "px)");
    tile.style.setProperty("--mid-x", "calc(var(--x) + " + dx * .52 + "px)");
    tile.style.setProperty("--mid-y", "calc(var(--y) + " + (dy * .52 - 28) + "px)");
    tile.style.setProperty("--fly-scale", scale);
  }

  function burstAtSlot(node, count) {
    var rect = node.getBoundingClientRect();
    spawnParticles(rect.left + rect.width / 2, rect.top + rect.height / 2, count);
  }

  function spawnParticles(x, y, count) {
    for (var i = 0; i < count; i += 1) {
      var angle = Math.PI * 2 * (i / count) + Math.random() * .45;
      var speed = 2.4 + Math.random() * 3.2;
      particles.push({
        x: x,
        y: y,
        vx: Math.cos(angle) * speed,
        vy: Math.sin(angle) * speed - 1.2,
        life: 44 + Math.random() * 18,
        age: 0,
        size: 4 + Math.random() * 8,
        spin: Math.random() * Math.PI,
        coin: i % 3 !== 0
      });
    }
    startFx();
  }

  function drawFx() {
    ctx.clearRect(0, 0, window.innerWidth, window.innerHeight);
    particles = particles.filter(function (p) {
      p.age += 1;
      p.x += p.vx;
      p.y += p.vy;
      p.vy += .1;
      p.spin += .18;
      var alpha = Math.max(0, 1 - p.age / p.life);
      ctx.save();
      ctx.globalAlpha = alpha;
      ctx.translate(p.x, p.y);
      ctx.rotate(p.spin);
      if (p.coin) {
        ctx.fillStyle = "#ffc421";
        ctx.strokeStyle = "#fff3a0";
        ctx.lineWidth = 2;
        ctx.beginPath();
        ctx.ellipse(0, 0, p.size, p.size * .72, 0, 0, Math.PI * 2);
        ctx.fill();
        ctx.stroke();
      } else {
        ctx.fillStyle = "#fff6a5";
        ctx.shadowColor = "#fff08a";
        ctx.shadowBlur = 10;
        ctx.fillRect(-p.size / 2, -p.size / 2, p.size, p.size);
      }
      ctx.restore();
      return p.age < p.life;
    });
    if (particles.length) {
      raf = window.requestAnimationFrame(drawFx);
    } else {
      raf = 0;
    }
  }

  function startFx() {
    if (!raf) raf = window.requestAnimationFrame(drawFx);
  }

  function formatTime(seconds) {
    var safe = Math.max(0, seconds);
    var minutes = Math.floor(safe / 60);
    var secs = safe % 60;
    return "0" + minutes + ":" + (secs < 10 ? "0" : "") + secs;
  }

  function updateTimerText() {
    if (timerText) timerText.textContent = formatTime(timeLeft);
  }

  function startTimer() {
    updateTimerText();
    if (timerId) window.clearInterval(timerId);
    timerId = window.setInterval(function () {
      if (locked) return;
      timeLeft -= 1;
      updateTimerText();
      if (timeLeft <= 0) {
        showEnd(false, "time");
      }
    }, 1000);
  }

  function stopTimer() {
    if (timerId) window.clearInterval(timerId);
    timerId = null;
  }

  function scheduleOpeningHint() {
    if (hintShown) return;
    hintShown = true;
    clearHint();
    hintTimer = window.setTimeout(showHint, 1050);
  }

  function clearHint() {
    if (hintTimer) window.clearTimeout(hintTimer);
    hintTimer = null;
    hand.classList.remove("show");
  }

  function showHint() {
    var tiles = Array.prototype.slice.call(board.querySelectorAll(".tile:not(.cleared)"));
    var boardRect = board.getBoundingClientRect();
    var target = tiles.reduce(function (best, tile) {
      if (!best) return tile;
      var tileRect = tile.getBoundingClientRect();
      var bestRect = best.getBoundingClientRect();
      if (Math.abs(tileRect.top - bestRect.top) > 6) {
        return tileRect.top < bestRect.top ? tile : best;
      }
      var center = boardRect.left + boardRect.width / 2;
      var tileDistance = Math.abs(tileRect.left + tileRect.width / 2 - center);
      var bestDistance = Math.abs(bestRect.left + bestRect.width / 2 - center);
      return tileDistance < bestDistance ? tile : best;
    }, null);
    if (!target) return;
    var rect = target.getBoundingClientRect();
    var stageRect = document.querySelector(".stage").getBoundingClientRect();
    hand.style.left = rect.left - stageRect.left + rect.width * .72 + "px";
    hand.style.top = rect.top - stageRect.top + rect.height * .62 + "px";
    hand.classList.add("show");
    window.setTimeout(function () {
      hand.classList.remove("show");
    }, 3200);
  }

  function showEnd(won, reason) {
    locked = true;
    clearHint();
    stopTimer();
    endTitle.textContent = won ? "Animal Mahjong" : reason === "time" ? "Time Up" : "Slots Full";
    endCopy.textContent = won ? "Triple-match every cute tile." : "Can you clear them all?";
    overlay.setAttribute("aria-hidden", "false");
    overlay.classList.add("show");
    if (won) playWin();
    else playLose();
    for (var i = 0; i < 8; i += 1) {
      window.setTimeout(function () {
        spawnParticles(window.innerWidth / 2, window.innerHeight / 2 - 85, 18);
      }, i * 90);
    }
  }

  function clickThrough() {
    getAudio();
    if (window.FbPlayableAd && typeof window.FbPlayableAd.onCTAClick === "function") {
      window.FbPlayableAd.onCTAClick();
      return;
    }
    if (window.mraid && typeof window.mraid.open === "function") {
      window.mraid.open(window.clickTag || window.installUrl || "");
      return;
    }
    if (window.dapi && typeof window.dapi.openStoreUrl === "function") {
      window.dapi.openStoreUrl();
      return;
    }
    if (window.clickTag || window.installUrl) {
      window.open(window.clickTag || window.installUrl, "_blank");
    }
  }

  cta.addEventListener("pointerdown", clickThrough);
  if (mainCta) mainCta.addEventListener("pointerdown", clickThrough);
  window.addEventListener("resize", function () {
    resizeCanvas();
    refreshTilePositions();
  });
  document.addEventListener("visibilitychange", function () {
    if (document.hidden && audioCtx) audioCtx.suspend();
  });

  resizeCanvas();
  buildBoard();
}());
