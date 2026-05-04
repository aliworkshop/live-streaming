(() => {
    const script = document.currentScript;
    const src = script.dataset.stream;
    const player = document.getElementById("player");

    if (player.canPlayType("application/vnd.apple.mpegurl")) {
        player.src = src;
    } else if (window.Hls && Hls.isSupported()) {
        const hls = new Hls({liveSyncDurationCount: 2});
        hls.loadSource(src);
        hls.attachMedia(player);
        hls.on(Hls.Events.ERROR, (_e, data) => {
            console.warn("hls error", data);
        });
    } else {
        player.outerHTML = "<p>HLS playback is not supported in this browser.</p>";
    }
})();
