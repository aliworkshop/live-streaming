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
        try {
            localStream = await navigator.mediaDevices.getUserMedia({audio: true, video: true});
        } catch (err) {
            setState(`cannot access camera: ${err.message}`);
            return;
        }
        localVideo.srcObject = localStream;
        isBroadcasting = true;
        goLiveBtn.hidden = true;
        stopLiveBtn.hidden = false;
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
        isBroadcasting = false;
        goLiveBtn.hidden = false;
        stopLiveBtn.hidden = true;
        viewerCountEl.textContent = "";
        setState("stopped");
    }

    function newBroadcasterPc(viewerId) {
        const pc = new RTCPeerConnection(RTC_CONFIG);
        for (const t of localStream.getTracks()) pc.addTrack(t, localStream);
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
    }

    // ---------- Viewer ----------
    function watch(streamId) {
        if (watchingId === streamId) return;
        if (watchingId) leave();
        watchingId = streamId;
        viewerPc = new RTCPeerConnection(RTC_CONFIG);
        viewerPc.ontrack = (e) => {
            remoteVideo.srcObject = e.streams[0];
            remoteVideo.hidden = false;
            leaveBtn.hidden = false;
        };
        viewerPc.onicecandidate = (e) => {
            if (e.candidate && watchingId) send({type: "ice", to: watchingId, payload: e.candidate});
        };
        viewerPc.onconnectionstatechange = () => {
            setState(`viewer connection: ${viewerPc.connectionState}`);
        };
        send({type: "watch", payload: {streamId}});
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
        remoteVideo.hidden = true;
        leaveBtn.hidden = true;
        watchingId = null;
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
                case "error":
                    setState(`error: ${typeof msg.payload === 'object' ? JSON.stringify(msg.payload) : msg.payload}`);
                    break;
            }
        });
    }

    connect();
})();
