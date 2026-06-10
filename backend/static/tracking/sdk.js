(function() {
  console.log("PlayableTracker initialized.");
  window.PlayableTracker = {
    trackEvent: function(name, value) {
      console.log("Track Event:", name, value);
      if (window.mraid && window.mraid.logEvent) {
        window.mraid.logEvent(name, value);
      }
    },
    trackGameStart: function() {
      this.trackEvent("game_start", {});
    },
    trackGameComplete: function(score) {
      this.trackEvent("game_complete", { score: score });
    },
    triggerCTA: function(fallbackUrl) {
      this.trackEvent("cta_click", {});
      if (window.mraid && window.mraid.open) {
        window.mraid.open(fallbackUrl);
      } else {
        window.open(fallbackUrl, "_blank");
      }
    }
  };
})();
