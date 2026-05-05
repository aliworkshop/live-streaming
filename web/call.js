(() => {
    const token = LS.getToken();
    const me = LS.getUser();
    const whoEl = document.getElementById("who");
    const stateEl = document.getElementById("state");
    const peersEl = document.getElementById("peers");
    const incomingSection = document.getElementById("incoming-section");
    const incomingFromEl = document.getElementById("incoming-from");
    const callSection = document.getElementById("call-section");
    const localVideo = document.getElementById("local-video");
    const remoteVideo = document.getElementById("remote-video");

    if (!token || !me) {
        whoEl.textContent = "you must log in on the home page first";
        return;
    }
    whoEl.textContent = `signed in as ${me.username}`;

    const RTC_CONFIG = {
        iceServers: [{urls: "stun:stun.l.google.com:19302"}],
    };

    let ws = null;
    let pc = null;
    let localStream = null;
    let peer = null; // { userId, username }
    let pendingOffer = null; // raw offer payload while waiting for accept

    function setState(msg) {
        stateEl.textContent = msg;
    }

    function send(msg) {
        if (ws && ws.readyState === WebSocket.OPEN) {
            ws.send(JSON.stringify(msg));
        }
    }

    function renderPeers(peers) {
        peersEl.innerHTML = "";
        if (!peers.length) {
            peersEl.innerHTML = "<li class='muted'>no peers online — open this page in another browser/profile</li>";
            return;
        }
        for (const p of peers) {
            const li = document.createElement("li");
            const btn = document.createElement("button");
            btn.textContent = `Call ${p.username}`;
            btn.addEventListener("click", () => startCall(p));
            li.appendChild(btn);
            peersEl.appendChild(li);
        }
    }

    async function ensureLocalStream() {
        if (localStream) return localStream;
        localStream = await navigator.mediaDevices.getUserMedia({audio: true, video: true});
        localVideo.srcObject = localStream;
        localVideo.classList.add("mirror");
        return localStream;
    }

    function newPeerConnection() {
        const conn = new RTCPeerConnection(RTC_CONFIG);
        conn.onicecandidate = (e) => {
            if (e.candidate && peer) {
                send({type: "ice", to: peer.userId, payload: e.candidate});
            }
        };
        conn.ontrack = (e) => {
            remoteVideo.srcObject = e.streams[0];
        };
        conn.onconnectionstatechange = () => {
            setState(`connection: ${conn.connectionState}`);
            if (["disconnected", "failed", "closed"].includes(conn.connectionState)) {
                endCall();
            }
        };
        return conn;
    }

    async function startCall(target) {
        if (pc) return;
        peer = target;
        setState(`calling ${peer.username}…`);
        callSection.hidden = false;
        await ensureLocalStream();
        pc = newPeerConnection();
        for (const t of localStream.getTracks()) pc.addTrack(t, localStream);
        send({type: "call-request", to: peer.userId});
        const offer = await pc.createOffer();
        await pc.setLocalDescription(offer);
        send({type: "offer", to: peer.userId, payload: offer});
    }

    async function acceptIncoming() {
        if (!pendingOffer || !peer) return;
        incomingSection.hidden = true;
        callSection.hidden = false;
        await ensureLocalStream();
        pc = newPeerConnection();
        for (const t of localStream.getTracks()) pc.addTrack(t, localStream);
        await pc.setRemoteDescription(new RTCSessionDescription(pendingOffer));
        pendingOffer = null;
        const answer = await pc.createAnswer();
        await pc.setLocalDescription(answer);
        send({type: "call-accept", to: peer.userId});
        send({type: "answer", to: peer.userId, payload: answer});
        setState(`in call with ${peer.username}`);
    }

    function rejectIncoming() {
        if (peer) send({type: "call-reject", to: peer.userId});
        pendingOffer = null;
        peer = null;
        incomingSection.hidden = true;
        setState("rejected");
    }

    function endCall() {
        if (peer) send({type: "call-end", to: peer.userId});
        if (pc) {
            pc.close();
            pc = null;
        }
        if (localStream) {
            for (const t of localStream.getTracks()) t.stop();
            localStream = null;
            localVideo.srcObject = null;
        }
        localVideo.classList.remove("mirror");
        remoteVideo.srcObject = null;
        callSection.hidden = true;
        incomingSection.hidden = true;
        peer = null;
        pendingOffer = null;
        setState("idle");
    }

    document.getElementById("accept").addEventListener("click", acceptIncoming);
    document.getElementById("reject").addEventListener("click", rejectIncoming);
    document.getElementById("hangup").addEventListener("click", endCall);

    function connect() {
        const proto = location.protocol === "https:" ? "wss" : "ws";
        ws = new WebSocket(`${proto}://${location.host}/ws/call?token=${encodeURIComponent(token)}`);
        ws.addEventListener("open", () => setState("connected to signaling"));
        ws.addEventListener("close", () => setState("disconnected — reload to retry"));
        ws.addEventListener("error", () => setState("signaling error"));

        ws.addEventListener("message", async (e) => {
            const msg = JSON.parse(e.data);
            switch (msg.type) {
                case "registered":
                    setState("registered");
                    break;
                case "peers":
                    renderPeers(msg.payload || []);
                    break;
                case "call-request":
                    // incoming call — show prompt, the actual offer arrives next
                    peer = {userId: msg.from, username: msg.from};
                    incomingFromEl.textContent = `incoming call from ${peer.username}`;
                    incomingSection.hidden = false;
                    break;
                case "offer":
                    pendingOffer = msg.payload;
                    peer = {userId: msg.from, username: msg.from};
                    // If no explicit call-request was sent first, still surface the prompt.
                    if (incomingSection.hidden) {
                        incomingFromEl.textContent = `incoming call from ${peer.username}`;
                        incomingSection.hidden = false;
                    }
                    break;
                case "answer":
                    if (pc) await pc.setRemoteDescription(new RTCSessionDescription(msg.payload));
                    setState(`in call with ${peer ? peer.username : "peer"}`);
                    break;
                case "ice":
                    if (pc && msg.payload) {
                        try {
                            await pc.addIceCandidate(msg.payload);
                        } catch (err) {
                            console.warn("ice add failed", err);
                        }
                    }
                    break;
                case "call-reject":
                    setState("call rejected");
                    endCall();
                    break;
                case "call-end":
                case "peer-offline":
                    setState(msg.type === "peer-offline" ? "peer offline" : "call ended");
                    endCall();
                    break;
                case "error":
                    setState(`error: ${msg.payload}`);
                    break;
            }
        });
    }

    connect();
})();
