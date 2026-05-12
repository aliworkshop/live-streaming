(() => {
    const channelSelect = document.getElementById("channel-select");
    const qualitySelect = document.getElementById("quality-select");
    const player = document.getElementById("player");
    const playerLogo = document.getElementById("player-logo");
    const stateEl = document.getElementById("state");

    let hls = null;
    let statusTimer = null;

    function setState(msg) {
        stateEl.textContent = msg;
    }

    function fmtKbps(bps) {
        if (!bps || !isFinite(bps)) return "—";
        const k = bps / 1000;
        if (k < 1000) return Math.round(k) + " kbps";
        return (k / 1000).toFixed(1) + " Mbps";
    }

    // Refresh the state line with the current ABR pick + bandwidth estimate
    // so users can verify ABR is actually reacting to network changes. When
    // the dropdown is on Auto we show "auto: 720p"; on a manual pick we say
    // "manual: 720p" so it's obvious why ABR isn't moving.
    function refreshStatus() {
        if (!hls) return;
        const manual = parseInt(qualitySelect.value, 10) !== -1;
        const lvl = hls.levels && hls.currentLevel >= 0 ? hls.levels[hls.currentLevel] : null;
        const h = lvl && lvl.height ? lvl.height + "p" : "—";
        const est = fmtKbps(hls.bandwidthEstimate);
        setState(`${manual ? "manual" : "auto"}: ${h} · estimated ${est}`);
    }

    function tearDownHls() {
        if (hls) {
            hls.destroy();
            hls = null;
        }
        if (statusTimer) {
            clearInterval(statusTimer);
            statusTimer = null;
        }
        // Reset quality dropdown — also reset to Auto so a previous manual
        // pick on a different channel doesn't pin ABR off on the new one.
        qualitySelect.innerHTML = '<option value="-1">Auto</option>';
        qualitySelect.value = "-1";
        qualitySelect.disabled = true;
    }

    function play(src) {
        tearDownHls();
        if (!src) return;
        setState("loading channel…");
        // Prefer HLS.js even on Safari (where native HLS also works) so we
        // can populate the Quality dropdown from data.levels. Native HLS
        // handles quality switching internally but doesn't expose the
        // variants to JavaScript, leaving our dropdown stuck on "Auto".
        // Native is only the fallback for browsers without Media Source
        // Extensions (mostly iOS Safari).
        if (window.Hls && Hls.isSupported()) {
            // lowLatencyMode triggers HLS.js's blocking-reload path which
            // expects the upstream to honour _HLS_msn / _HLS_part. Most
            // upstreams (and our naive HTTP proxy) don't, so HLS.js stalls.
            // Plain polling mode is the reliable default.
            //
            // ABR tuning: HLS.js's defaults for live (`abrEwmaFastLive=3.0`,
            // `abrEwmaSlowLive=9.0`) make bandwidth changes take 5–15 s to
            // register. Tightening to 1 s / 3 s makes it react in roughly a
            // segment-and-a-half. `abrBandWidthUpFactor=0.7` is left at
            // default — switching UP needs more headroom than down to avoid
            // re-buffering.
            hls = new Hls({
                lowLatencyMode: false,
                abrEwmaFastLive: 1.0,
                abrEwmaSlowLive: 3.0,
                abrBandWidthFactor: 0.95,
                abrBandWidthUpFactor: 0.7,
            });
            hls.loadSource(src);
            hls.attachMedia(player);
        } else if (player.canPlayType("application/vnd.apple.mpegurl")) {
            // Fallback for iOS Safari where MSE isn't fully available;
            // quality switching is then handled by the native player.
            player.src = src;
            qualitySelect.disabled = true;
            setState("playing (native HLS — quality picked by browser)");
            return;
        } else {
            setState("HLS not supported in this browser");
            return;
        }

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
            qualitySelect.value = "-1";   // start in Auto so ABR is on
            // Start a 1 s ticker so the status line reflects the current
            // ABR pick and bandwidth estimate continuously — without this,
            // it's hard to tell whether ABR is actually reacting.
            if (statusTimer) clearInterval(statusTimer);
            statusTimer = setInterval(refreshStatus, 1000);
            refreshStatus();
        });

        hls.on(Hls.Events.LEVEL_SWITCHED, refreshStatus);

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
        // -1 == auto (ABR on); any other value pins that variant (ABR off).
        hls.currentLevel = v;
        refreshStatus();
    });

    function updateLogo() {
        const opt = channelSelect.selectedOptions[0];
        const logo = opt && opt.dataset ? opt.dataset.logo : "";
        if (logo) {
            playerLogo.src = logo;
            playerLogo.hidden = false;
        } else {
            playerLogo.removeAttribute("src");
            playerLogo.hidden = true;
        }
    }

    channelSelect.addEventListener("change", () => {
        updateLogo();
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
                if (ch.logo) opt.dataset.logo = ch.logo;
                channelSelect.appendChild(opt);
            }
            updateLogo();
            play(channelSelect.value);
        } catch (err) {
            setState(`failed to load channels: ${err.message}`);
        }
    }

    loadChannels();
})();
