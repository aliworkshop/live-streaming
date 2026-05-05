(() => {
    const channelSelect = document.getElementById("channel-select");
    const qualitySelect = document.getElementById("quality-select");
    const player = document.getElementById("player");
    const stateEl = document.getElementById("state");

    let hls = null;

    function setState(msg) {
        stateEl.textContent = msg;
    }

    function tearDownHls() {
        if (hls) {
            hls.destroy();
            hls = null;
        }
        // Reset quality dropdown
        qualitySelect.innerHTML = '<option value="-1">Auto</option>';
        qualitySelect.disabled = true;
    }

    function play(src) {
        tearDownHls();
        if (!src) return;
        if (player.canPlayType("application/vnd.apple.mpegurl")) {
            // Safari & iOS handle HLS natively — quality selection is
            // automatic and not exposed via JS.
            player.src = src;
            qualitySelect.disabled = true;
            setState("playing (native HLS)");
            return;
        }
        if (!window.Hls || !Hls.isSupported()) {
            setState("HLS not supported in this browser");
            return;
        }
        // lowLatencyMode triggers HLS.js's blocking-reload path which expects
        // the upstream to honour _HLS_msn / _HLS_part. Most upstreams (and
        // our naive HTTP proxy) don't, so HLS.js just stalls. Plain polling
        // mode is the reliable default.
        hls = new Hls({lowLatencyMode: false});
        hls.loadSource(src);
        hls.attachMedia(player);

        hls.on(Hls.Events.MANIFEST_PARSED, (_e, data) => {
            // Populate quality picker from the variant levels in the master.
            qualitySelect.innerHTML = '<option value="-1">Auto</option>';
            (data.levels || []).forEach((level, i) => {
                const opt = document.createElement("option");
                opt.value = String(i);
                const h = level.height ? level.height + "p" : "";
                const k = level.bitrate ? Math.round(level.bitrate / 1000) + " kbps" : "";
                opt.textContent = [h, k].filter(Boolean).join(" · ") || `Level ${i}`;
                qualitySelect.appendChild(opt);
            });
            qualitySelect.disabled = data.levels.length < 2;
            setState(`playing — ${data.levels.length} variant(s)`);
        });

        hls.on(Hls.Events.LEVEL_SWITCHED, (_e, data) => {
            if (parseInt(qualitySelect.value, 10) === -1) {
                const lvl = hls.levels[data.level];
                if (lvl && lvl.height) setState(`auto: ${lvl.height}p`);
            }
        });

        hls.on(Hls.Events.ERROR, (_e, data) => {
            console.warn("hls error", data);
            if (data.fatal) {
                setState(`fatal: ${data.type} / ${data.details}`);
            }
        });
    }

    qualitySelect.addEventListener("change", () => {
        if (!hls) return;
        const v = parseInt(qualitySelect.value, 10);
        // -1 == auto
        hls.currentLevel = v;
    });

    channelSelect.addEventListener("change", () => {
        play(channelSelect.value);
    });

    async function loadChannels() {
        try {
            const res = await fetch("/api/external");
            if (!res.ok) throw new Error(`HTTP ${res.status}`);
            const list = await res.json();
            channelSelect.innerHTML = "";
            if (!list.length) {
                channelSelect.innerHTML = "<option>(no channels configured)</option>";
                channelSelect.disabled = true;
                setState("add channels under externalChannels in config");
                return;
            }
            for (const ch of list) {
                const opt = document.createElement("option");
                opt.value = ch.url;
                opt.textContent = ch.name;
                channelSelect.appendChild(opt);
            }
            play(channelSelect.value);
        } catch (err) {
            setState(`failed to load channels: ${err.message}`);
        }
    }

    loadChannels();
})();
