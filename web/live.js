(() => {
    const token = LS.getToken();
    const me = LS.getUser();
    const whoEl = document.getElementById("who");
    const stateEl = document.getElementById("state");
    const streamsEl = document.getElementById("streams");
    const goLiveForm = document.getElementById("go-live-form");
    const goLiveBtn = document.getElementById("go-live-btn");
    const stopLiveBtn = document.getElementById("stop-live-btn");
    const localVideo = document.getElementById("local-video");
    const remoteVideo = document.getElementById("remote-video");
    const viewerCountEl = document.getElementById("viewer-count");
    const leaveBtn = document.getElementById("leave-btn");
    const screenShareToggle = document.getElementById("screen-share-toggle");
    const slideSection = document.getElementById("slide-section");
    const slideControls = document.getElementById("slide-controls");
    const slideFile = document.getElementById("slide-file");
    const slidePrev = document.getElementById("slide-prev");
    const slideNext = document.getElementById("slide-next");
    const slidePageInfo = document.getElementById("slide-page-info");
    const slideCanvas = document.getElementById("slide-canvas");
    const stageSection = document.getElementById("stage-section");
    const chatList = document.getElementById("chat-list");
    const chatForm = document.getElementById("chat-form");
    const chatInput = document.getElementById("chat-input");
    const handControls = document.getElementById("hand-controls");
    const raiseHandBtn = document.getElementById("raise-hand-btn");
    const handQueueSection = document.getElementById("hand-queue-section");
    const handQueueEl = document.getElementById("hand-queue");
    const speakerInfo = document.getElementById("speaker-info");
    const speakerNameEl = document.getElementById("speaker-name");
    const speakerPip = document.getElementById("speaker-pip");
    const revokeSpeakerBtn = document.getElementById("revoke-speaker-btn");

    function refreshStage() {
        const active = isBroadcasting || !!watchingId;
        stageSection.hidden = !active;
        localVideo.hidden = !isBroadcasting;
        remoteVideo.hidden = !watchingId;
    }

    if (!token || !me) {
        whoEl.textContent = "you must log in on the home page first";
        return;
    }
    whoEl.textContent = `signed in as ${me.username}`;

    const RTC_CONFIG = {iceServers: [{urls: "stun:stun.l.google.com:19302"}]};

    let ws = null;
    // Broadcaster state
    let localStream = null;
    let isBroadcasting = false;
    const peerConnsByViewer = new Map(); // viewerId -> RTCPeerConnection
    // Viewer state
    let watchingId = null;
    let viewerPc = null;

    // Slide state (shared by broadcaster and viewer)
    let pdfDoc = null;
    let pdfUrl = null;
    let pdfPage = 1;

    // Phase 2: raise-hand / promote-to-speaker
    let myHandState = "none";              // "none" | "raised" | "speaking"
    let speakerStream = null;              // student-side: my own back-channel media
    let backChannelPc = null;              // student-side: my PC to the broadcaster
    const backChannelByViewer = new Map(); // teacher-side: viewerId -> back-channel PC

    // Teacher-side fan-out: the speaker's MediaStream is mirrored into every
    // viewer's forward PC so the whole class can hear/see them.
    let currentSpeakerStream = null;       // teacher-side: speaker's media we received
    let currentSpeakerId = null;           // teacher-side: userId of the active speaker (skip when fanning out)
    const viewerSpeakerSenders = new Map(); // teacher-side: viewerId -> RTCRtpSender[] for the speaker tracks
    let speakerFanoutTimer = null;

    // Viewer-side: distinguish broadcaster's stream from the speaker's stream
    // by stream id. First stream we see is the broadcaster; any later stream
    // with a different id is the speaker.
    let mainRemoteStreamId = null;

    function setState(msg) {
        stateEl.textContent = msg;
    }

    function send(msg) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify(msg));
        }
    }

    function renderStreams(streams) {
        streamsEl.innerHTML = "";
        if (!streams.length) {
            streamsEl.innerHTML = "<li class='muted'>no active streams</li>";
            return;
        }
        for (const s of streams) {
            const li = document.createElement("li");
            const label = document.createElement("span");
            label.textContent = `${s.broadcaster}${s.title ? " — " + s.title : ""} · ${s.viewers} viewer(s) `;
            const btn = document.createElement("button");
            const isMine = s.streamId === me.id;
            btn.textContent = isMine ? "your stream" : (watchingId === s.streamId ? "watching" : "Watch");
            btn.disabled = isMine || watchingId === s.streamId;
            btn.addEventListener("click", () => watch(s.streamId));
            li.appendChild(label);
            li.appendChild(btn);
            streamsEl.appendChild(li);
        }
        if (isBroadcasting) {
            const me_ = streams.find((s) => s.streamId === me.id);
            viewerCountEl.textContent = me_ ? `${me_.viewers} viewer(s) watching` : "0 viewers";
        }
    }

    // ---------- Broadcaster ----------
    goLiveForm.addEventListener("submit", async (e) => {
        e.preventDefault();
        if (isBroadcasting) return;
        const title = new FormData(e.target).get("title") || "";
        const wantScreen = screenShareToggle && screenShareToggle.checked;
        try {
            localStream = wantScreen
                ? await navigator.mediaDevices.getDisplayMedia({video: true, audio: true})
                : await navigator.mediaDevices.getUserMedia({audio: true, video: true});
        } catch (err) {
            setState(`cannot access ${wantScreen ? "screen" : "camera"}: ${err.message}`);
            return;
        }
        // If the user stops the screen-share via the browser UI we treat it as a hangup.
        for (const track of localStream.getVideoTracks()) {
            track.addEventListener("ended", () => stopBroadcast());
        }
        localVideo.srcObject = localStream;
        // Mirror the camera self-view but not a screen capture.
        localVideo.classList.toggle("mirror", !wantScreen);
        isBroadcasting = true;
        goLiveBtn.hidden = true;
        stopLiveBtn.hidden = false;
        slideSection.hidden = false;
        slideControls.hidden = false;
        refreshStage();
        renderHandQueue([]);
        send({type: "go-live", payload: {title}});
        setState("broadcasting");
    });

    stopLiveBtn.addEventListener("click", () => stopBroadcast());

    function stopBroadcast() {
        if (!isBroadcasting) return;
        send({type: "stop-live"});
        for (const pc of peerConnsByViewer.values()) pc.close();
        peerConnsByViewer.clear();
        if (localStream) {
            for (const t of localStream.getTracks()) t.stop();
            localStream = null;
            localVideo.srcObject = null;
        }
        localVideo.classList.remove("mirror");
        isBroadcasting = false;
        goLiveBtn.hidden = false;
        stopLiveBtn.hidden = true;
        viewerCountEl.textContent = "";
        slideControls.hidden = true;
        slideSection.hidden = true;
        clearSlide();
        tearDownTeacherBackChannels();
        renderHandQueue([]);
        renderSpeaker(null);
        refreshStage();
        setState("stopped");
    }

    function newBroadcasterPc(viewerId) {
        const pc = new RTCPeerConnection(RTC_CONFIG);
        for (const t of localStream.getTracks()) pc.addTrack(t, localStream);
        // If a student is already speaking when this viewer joins, include
        // their tracks too so the new viewer sees the speaker from the start.
        if (currentSpeakerStream && viewerId !== currentSpeakerId) {
            const senders = [];
            for (const t of currentSpeakerStream.getTracks()) {
                try {
                    senders.push(pc.addTrack(t, currentSpeakerStream));
                } catch {
                }
            }
            if (senders.length) viewerSpeakerSenders.set(viewerId, senders);
        }
        pc.onicecandidate = (e) => {
            if (e.candidate) send({type: "ice", to: viewerId, payload: e.candidate});
        };
        pc.onconnectionstatechange = () => {
            if (["disconnected", "failed", "closed"].includes(pc.connectionState)) {
                const old = peerConnsByViewer.get(viewerId);
                if (old) {
                    old.close();
                    peerConnsByViewer.delete(viewerId);
                }
                viewerSpeakerSenders.delete(viewerId);
            }
        };
        peerConnsByViewer.set(viewerId, pc);
        return pc;
    }

    async function onViewerJoined(viewerId) {
        if (!isBroadcasting || !localStream) return;
        const pc = newBroadcasterPc(viewerId);
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        send({type: "offer", to: viewerId, payload: offer});
    }

    function onViewerLeft(viewerId) {
        const pc = peerConnsByViewer.get(viewerId);
        if (pc) {
            pc.close();
            peerConnsByViewer.delete(viewerId);
        }
        viewerSpeakerSenders.delete(viewerId);
    }

    // ---------- Viewer ----------
    function watch(streamId) {
        if (watchingId === streamId) return;
        if (watchingId) leave();
        watchingId = streamId;
        viewerPc = new RTCPeerConnection(RTC_CONFIG);
        viewerPc.ontrack = (e) => {
            const stream = e.streams[0];
            if (!stream) return;
            if (!mainRemoteStreamId) {
                mainRemoteStreamId = stream.id;
                remoteVideo.srcObject = stream;
                leaveBtn.hidden = false;
                refreshStage();
            } else if (stream.id !== mainRemoteStreamId) {
                // Second stream = active speaker, mirrored by the broadcaster.
                speakerPip.srcObject = stream;
                speakerPip.hidden = false;
            } else if (remoteVideo.srcObject !== stream) {
                remoteVideo.srcObject = stream;
            }
        };
        viewerPc.onicecandidate = (e) => {
            if (e.candidate && watchingId) send({type: "ice", to: watchingId, payload: e.candidate});
        };
        viewerPc.onconnectionstatechange = () => {
            setState(`viewer connection: ${viewerPc.connectionState}`);
        };
        send({type: "watch", payload: {streamId}});
        refreshStage();
        renderHandControls();
        setState(`joining stream ${streamId}…`);
    }

    function leave() {
        if (!watchingId) return;
        send({type: "leave"});
        if (viewerPc) {
            viewerPc.close();
            viewerPc = null;
        }
        remoteVideo.srcObject = null;
        leaveBtn.hidden = true;
        watchingId = null;
        mainRemoteStreamId = null;
        slideSection.hidden = true;
        clearSlide();
        tearDownStudentBackChannel();
        myHandState = "none";
        renderHandControls();
        renderSpeaker(null);
        refreshStage();
        setState("left stream");
    }

    leaveBtn.addEventListener("click", leave);

    async function onViewerOffer(from, sdp) {
        if (!viewerPc || from !== watchingId) return;
        await viewerPc.setRemoteDescription(new RTCSessionDescription(sdp));
        const answer = await viewerPc.createAnswer();
        await viewerPc.setLocalDescription(answer);
        send({type: "answer", to: from, payload: answer});
    }

    async function onBroadcasterAnswer(from, sdp) {
        const pc = peerConnsByViewer.get(from);
        if (!pc) return;
        await pc.setRemoteDescription(new RTCSessionDescription(sdp));
    }

    async function onIce(from, candidate) {
        if (isBroadcasting && peerConnsByViewer.has(from)) {
            try {
                await peerConnsByViewer.get(from).addIceCandidate(candidate);
            } catch {
            }
        } else if (viewerPc && from === watchingId) {
            try {
                await viewerPc.addIceCandidate(candidate);
            } catch {
            }
        }
    }

    // ---------- Slides (broadcaster + viewer) ----------
    async function uploadPdf(file) {
        const fd = new FormData();
        fd.append("file", file);
        const res = await fetch("/api/live/upload", {
            method: "POST",
            headers: {"Authorization": `Bearer ${token}`},
            body: fd,
        });
        if (!res.ok) {
            const text = await res.text();
            throw new Error(text || `HTTP ${res.status}`);
        }
        return (await res.json()).url;
    }

    async function loadPdf(url) {
        if (!window.pdfjsLib) throw new Error("pdf.js not loaded");
        if (pdfUrl === url && pdfDoc) return pdfDoc;
        pdfDoc = await pdfjsLib.getDocument(url).promise;
        pdfUrl = url;
        return pdfDoc;
    }

    async function renderPage(page) {
        if (!pdfDoc) return;
        const clamped = Math.max(1, Math.min(page, pdfDoc.numPages));
        const pageObj = await pdfDoc.getPage(clamped);
        const viewport = pageObj.getViewport({scale: 1.5});
        const ctx = slideCanvas.getContext("2d");
        slideCanvas.width = viewport.width;
        slideCanvas.height = viewport.height;
        await pageObj.render({canvasContext: ctx, viewport}).promise;
        pdfPage = clamped;
        slidePageInfo.textContent = `${clamped} / ${pdfDoc.numPages}`;
    }

    function clearSlide() {
        pdfDoc = null;
        pdfUrl = null;
        pdfPage = 1;
        slidePageInfo.textContent = "– / –";
        const ctx = slideCanvas.getContext("2d");
        ctx.clearRect(0, 0, slideCanvas.width, slideCanvas.height);
    }

    function pushSlide() {
        if (!isBroadcasting || !pdfUrl) return;
        send({type: "slide", payload: {url: pdfUrl, page: pdfPage}});
    }

    slideFile.addEventListener("change", async (e) => {
        const file = e.target.files[0];
        if (!file) return;
        try {
            const url = await uploadPdf(file);
            await loadPdf(url);
            await renderPage(1);
            pushSlide();
            setState(`slides loaded (${pdfDoc.numPages} pages)`);
        } catch (err) {
            setState(`slide upload failed: ${err.message}`);
        }
    });

    slidePrev.addEventListener("click", async () => {
        if (!pdfDoc) return;
        await renderPage(pdfPage - 1);
        pushSlide();
    });

    slideNext.addEventListener("click", async () => {
        if (!pdfDoc) return;
        await renderPage(pdfPage + 1);
        pushSlide();
    });

    async function onSlide(payload) {
        if (!payload || !payload.url) return;
        slideSection.hidden = false;
        try {
            await loadPdf(payload.url);
            await renderPage(payload.page || 1);
        } catch (err) {
            setState(`slide render failed: ${err.message}`);
        }
    }

    // ---------- Chat ----------
    function escapeHtml(s) {
        return String(s).replace(/[&<>"']/g, (c) => ({
            "&": "&amp;", "<": "&lt;", ">": "&gt;", "\"": "&quot;", "'": "&#39;",
        })[c]);
    }

    function appendChat({from, text, at}) {
        const li = document.createElement("li");
        const ts = at ? new Date(at).toLocaleTimeString() : "";
        li.innerHTML =
            `<strong>${escapeHtml(from || "?")}</strong>` +
            ` <span class="muted">${ts}</span><br>` +
            escapeHtml(text || "");
        chatList.appendChild(li);
        chatList.scrollTop = chatList.scrollHeight;
    }

    chatForm.addEventListener("submit", (e) => {
        e.preventDefault();
        const text = chatInput.value.trim();
        if (!text) return;
        if (!isBroadcasting && !watchingId) {
            setState("join or start a stream first");
            return;
        }
        send({type: "chat", payload: {text}});
        chatInput.value = "";
    });

    // ---------- Phase 2: raise-hand / speaker ----------
    function renderHandControls() {
        if (!watchingId || isBroadcasting) {
            handControls.hidden = true;
            return;
        }
        handControls.hidden = false;
        if (myHandState === "raised") {
            raiseHandBtn.textContent = "✋ Lower hand";
        } else if (myHandState === "speaking") {
            raiseHandBtn.textContent = "🎤 Stop speaking";
        } else {
            raiseHandBtn.textContent = "✋ Raise hand";
        }
    }

    function renderHandQueue(hands) {
        if (!isBroadcasting) {
            handQueueSection.hidden = true;
            return;
        }
        handQueueSection.hidden = false;
        handQueueEl.innerHTML = "";
        if (!hands.length) {
            const li = document.createElement("li");
            li.className = "muted";
            li.textContent = "no hands raised";
            handQueueEl.appendChild(li);
            return;
        }
        for (const h of hands) {
            const li = document.createElement("li");
            const label = document.createElement("span");
            label.textContent = h.username;
            label.style.flex = "1";
            const accept = document.createElement("button");
            accept.textContent = "Accept";
            accept.addEventListener("click", () => send({type: "accept-hand", to: h.userId}));
            const reject = document.createElement("button");
            reject.textContent = "✕";
            reject.title = "reject";
            reject.addEventListener("click", () => send({type: "reject-hand", to: h.userId}));
            li.append(label, accept, reject);
            handQueueEl.appendChild(li);
        }
    }

    function renderSpeaker(speaker) {
        if (!speaker) {
            speakerInfo.hidden = true;
            speakerNameEl.textContent = "";
            speakerPip.srcObject = null;
            speakerPip.muted = false;
            speakerPip.classList.remove("mirror");
            speakerPip.hidden = true;
            revokeSpeakerBtn.hidden = true;
            return;
        }
        speakerInfo.hidden = false;
        speakerNameEl.textContent = speaker.username;
        if (isBroadcasting) {
            revokeSpeakerBtn.hidden = false;
            // pip becomes visible when the back-channel offer arrives & ontrack fires
        } else {
            revokeSpeakerBtn.hidden = true;
        }
    }

    raiseHandBtn.addEventListener("click", () => {
        if (!watchingId) return;
        if (myHandState === "raised" || myHandState === "speaking") {
            // Server treats lower-hand as both "drop from queue" and "stop speaking".
            send({type: "lower-hand"});
            if (myHandState === "raised") {
                myHandState = "none";
                renderHandControls();
            }
        } else {
            send({type: "raise-hand"});
            myHandState = "raised";
            setState("hand raised");
            renderHandControls();
        }
    });

    revokeSpeakerBtn.addEventListener("click", () => {
        if (!isBroadcasting) return;
        send({type: "revoke-speaker"});
    });

    function tearDownStudentBackChannel() {
        if (backChannelPc) {
            backChannelPc.close();
            backChannelPc = null;
        }
        if (speakerStream) {
            for (const t of speakerStream.getTracks()) t.stop();
            speakerStream = null;
        }
    }

    function tearDownTeacherBackChannels() {
        for (const pc of backChannelByViewer.values()) pc.close();
        backChannelByViewer.clear();
        speakerPip.srcObject = null;
        speakerPip.hidden = true;
        if (speakerFanoutTimer) {
            clearTimeout(speakerFanoutTimer);
            speakerFanoutTimer = null;
        }
        removeSpeakerFromAllViewers();
        currentSpeakerId = null;
    }

    async function onHandAccepted(broadcasterId) {
        try {
            speakerStream = await navigator.mediaDevices.getUserMedia({audio: true, video: true});
        } catch (err) {
            setState(`cannot access camera/mic for speaking: ${err.message}`);
            send({type: "lower-hand"});
            myHandState = "none";
            renderHandControls();
            return;
        }
        // Self-view: show our own camera in the pip. Mute so we don't hear
        // ourselves as feedback (the teacher hears us via the back-channel).
        speakerPip.srcObject = speakerStream;
        speakerPip.muted = true;
        speakerPip.classList.add("mirror");
        speakerPip.hidden = false;
        backChannelPc = new RTCPeerConnection(RTC_CONFIG);
        for (const t of speakerStream.getTracks()) backChannelPc.addTrack(t, speakerStream);
        backChannelPc.onicecandidate = (e) => {
            if (e.candidate) send({type: "back-ice", to: broadcasterId, payload: e.candidate});
        };
        backChannelPc.onconnectionstatechange = () => {
            if (backChannelPc) setState(`back-channel: ${backChannelPc.connectionState}`);
        };
        const offer = await backChannelPc.createOffer();
        await backChannelPc.setLocalDescription(offer);
        send({type: "back-offer", to: broadcasterId, payload: offer});
        myHandState = "speaking";
        renderHandControls();
        setState("you are now speaking");
    }

    function onHandRejected() {
        myHandState = "none";
        renderHandControls();
        setState("hand request rejected");
    }

    function onSpeakerRevoked() {
        if (myHandState !== "none") {
            tearDownStudentBackChannel();
            myHandState = "none";
            renderHandControls();
            setState("speaker turn ended");
        }
    }

    function handleSpeakerUpdate(speaker) {
        renderSpeaker(speaker);
        if (isBroadcasting) {
            currentSpeakerId = speaker ? speaker.userId : null;
            if (!speaker) {
                tearDownTeacherBackChannels();
            }
        }
    }

    // Re-issue an offer to a viewer after we've added/removed tracks from
    // their forward PC (used to push speaker media to the whole class).
    async function renegotiateViewer(viewerId) {
        const pc = peerConnsByViewer.get(viewerId);
        if (!pc) return;
        try {
            const offer = await pc.createOffer();
            await pc.setLocalDescription(offer);
            send({type: "offer", to: viewerId, payload: offer});
        } catch (err) {
            console.warn("renegotiate failed for", viewerId, err);
        }
    }

    // Mirror the active speaker's tracks into every viewer's forward PC
    // (skipping the speaker themselves so we don't loop them back).
    function fanOutSpeakerToViewers() {
        if (!currentSpeakerStream) return;
        for (const [viewerId, viewerPc] of peerConnsByViewer) {
            if (viewerId === currentSpeakerId) continue;
            // Drop any previous senders we added for this stream so re-runs
            // (multiple ontracks, debounced) stay idempotent.
            const prev = viewerSpeakerSenders.get(viewerId);
            if (prev) {
                for (const s of prev) {
                    try {
                        viewerPc.removeTrack(s);
                    } catch {
                    }
                }
            }
            const senders = [];
            for (const t of currentSpeakerStream.getTracks()) {
                try {
                    senders.push(viewerPc.addTrack(t, currentSpeakerStream));
                } catch {
                }
            }
            viewerSpeakerSenders.set(viewerId, senders);
            renegotiateViewer(viewerId);
        }
    }

    function removeSpeakerFromAllViewers() {
        for (const [viewerId, senders] of viewerSpeakerSenders) {
            const viewerPc = peerConnsByViewer.get(viewerId);
            if (!viewerPc) continue;
            for (const s of senders) {
                try {
                    viewerPc.removeTrack(s);
                } catch {
                }
            }
            renegotiateViewer(viewerId);
        }
        viewerSpeakerSenders.clear();
        currentSpeakerStream = null;
    }

    async function onBackOffer(viewerId, sdp) {
        if (!isBroadcasting) return;
        const pc = new RTCPeerConnection(RTC_CONFIG);
        pc.ontrack = (e) => {
            const stream = e.streams[0];
            if (!stream) return;
            speakerPip.srcObject = stream;
            speakerPip.hidden = false;
            currentSpeakerStream = stream;
            // Multiple ontracks (audio + video) fire close together; debounce
            // the fan-out so we re-add tracks once both have arrived.
            if (speakerFanoutTimer) clearTimeout(speakerFanoutTimer);
            speakerFanoutTimer = setTimeout(() => {
                speakerFanoutTimer = null;
                fanOutSpeakerToViewers();
            }, 150);
        };
        pc.onicecandidate = (e) => {
            if (e.candidate) send({type: "back-ice", to: viewerId, payload: e.candidate});
        };
        pc.onconnectionstatechange = () => {
            if (["disconnected", "failed", "closed"].includes(pc.connectionState)) {
                const old = backChannelByViewer.get(viewerId);
                if (old) {
                    old.close();
                    backChannelByViewer.delete(viewerId);
                }
                if (backChannelByViewer.size === 0) {
                    speakerPip.srcObject = null;
                    speakerPip.hidden = true;
                }
            }
        };
        backChannelByViewer.set(viewerId, pc);
        await pc.setRemoteDescription(new RTCSessionDescription(sdp));
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        send({type: "back-answer", to: viewerId, payload: answer});
    }

    async function onBackAnswer(_from, sdp) {
        if (!backChannelPc) return;
        await backChannelPc.setRemoteDescription(new RTCSessionDescription(sdp));
    }

    async function onBackIce(from, candidate) {
        if (isBroadcasting && backChannelByViewer.has(from)) {
            try {
                await backChannelByViewer.get(from).addIceCandidate(candidate);
            } catch {
            }
        } else if (backChannelPc) {
            try {
                await backChannelPc.addIceCandidate(candidate);
            } catch {
            }
        }
    }

    // ---------- Connect ----------
    function connect() {
        const proto = location.protocol === "https:" ? "wss" : "ws";
        ws = new WebSocket(`${proto}://${location.host}/ws/live?token=${encodeURIComponent(token)}`);
        ws.addEventListener("open", () => setState("connected"));
        ws.addEventListener("close", () => setState("disconnected — reload to retry"));
        ws.addEventListener("error", () => setState("signaling error"));

        ws.addEventListener("message", async (e) => {
            const msg = JSON.parse(e.data);
            switch (msg.type) {
                case "registered":
                    setState("registered");
                    break;
                case "live-list":
                    renderStreams(msg.payload || []);
                    break;
                case "viewer-joined":
                    await onViewerJoined(msg.from);
                    break;
                case "viewer-left":
                    onViewerLeft(msg.from);
                    break;
                case "offer":
                    await onViewerOffer(msg.from, msg.payload);
                    break;
                case "answer":
                    await onBroadcasterAnswer(msg.from, msg.payload);
                    break;
                case "ice":
                    await onIce(msg.from, msg.payload);
                    break;
                case "stream-offline":
                    if (watchingId && msg.payload && msg.payload.streamId === watchingId) {
                        setState("stream went offline");
                        leave();
                    }
                    break;
                case "chat":
                    appendChat(msg.payload || {});
                    break;
                case "slide":
                    await onSlide(msg.payload);
                    break;
                case "hands-update":
                    renderHandQueue((msg.payload && msg.payload.hands) || []);
                    break;
                case "speaker-update":
                    handleSpeakerUpdate(msg.payload && msg.payload.speaker);
                    break;
                case "hand-accepted":
                    await onHandAccepted(msg.from);
                    break;
                case "hand-rejected":
                    onHandRejected();
                    break;
                case "speaker-revoked":
                    onSpeakerRevoked();
                    break;
                case "back-offer":
                    await onBackOffer(msg.from, msg.payload);
                    break;
                case "back-answer":
                    await onBackAnswer(msg.from, msg.payload);
                    break;
                case "back-ice":
                    await onBackIce(msg.from, msg.payload);
                    break;
                case "error":
                    setState(`error: ${typeof msg.payload === 'object' ? JSON.stringify(msg.payload) : msg.payload}`);
                    break;
            }
        });
    }

    connect();
})();
