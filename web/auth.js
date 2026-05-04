const TOKEN_KEY = "ls.token";
const USER_KEY = "ls.user";

function setSession(token, user) {
    localStorage.setItem(TOKEN_KEY, token);
    localStorage.setItem(USER_KEY, JSON.stringify(user));
}

function getToken() {
    return localStorage.getItem(TOKEN_KEY);
}

function getUser() {
    const u = localStorage.getItem(USER_KEY);
    return u ? JSON.parse(u) : null;
}

function clearSession() {
    localStorage.removeItem(TOKEN_KEY);
    localStorage.removeItem(USER_KEY);
}

async function api(path, opts = {}) {
    const headers = Object.assign({"Content-Type": "application/json"}, opts.headers || {});
    const token = getToken();
    if (token) headers["Authorization"] = `Bearer ${token}`;
    const res = await fetch(path, Object.assign({}, opts, {headers}));
    const text = await res.text();
    let body;
    try {
        body = text ? JSON.parse(text) : null;
    } catch {
        body = text;
    }
    if (!res.ok) throw new Error(body && body.error ? body.error : `HTTP ${res.status}`);
    return body;
}

window.LS = {api, getToken, getUser, setSession, clearSession};

if (document.getElementById("signup-form")) {
    const status = document.getElementById("auth-status");
    const meSection = document.getElementById("me-section");
    const meEl = document.getElementById("me");

    async function refreshMe() {
        if (!getToken()) {
            meSection.hidden = true;
            return;
        }
        try {
            const me = await api("/api/me");
            meSection.hidden = false;
            meEl.textContent = JSON.stringify(me, null, 2);
        } catch (err) {
            clearSession();
            meSection.hidden = true;
            status.textContent = `session expired: ${err.message}`;
        }
    }

    document.getElementById("signup-form").addEventListener("submit", async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        try {
            const r = await api("/api/signup", {
                method: "POST",
                body: JSON.stringify({
                    username: fd.get("username"),
                    password: fd.get("password"),
                    displayName: fd.get("displayName"),
                }),
            });
            setSession(r.token, r.user);
            status.textContent = `signed up as ${r.user.username}`;
            refreshMe();
        } catch (err) {
            status.textContent = `signup failed: ${err.message}`;
        }
    });

    document.getElementById("login-form").addEventListener("submit", async (e) => {
        e.preventDefault();
        const fd = new FormData(e.target);
        try {
            const r = await api("/api/login", {
                method: "POST",
                body: JSON.stringify({
                    username: fd.get("username"),
                    password: fd.get("password"),
                }),
            });
            setSession(r.token, r.user);
            status.textContent = `logged in as ${r.user.username}`;
            refreshMe();
        } catch (err) {
            status.textContent = `login failed: ${err.message}`;
        }
    });

    document.getElementById("logout").addEventListener("click", () => {
        clearSession();
        location.reload();
    });

    refreshMe();
}
