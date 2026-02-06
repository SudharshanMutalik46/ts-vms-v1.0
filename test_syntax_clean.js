        let overlayEnabled = false;
        let weaponEnabled = false;

        function toggleOverlay() {
            const video = document.getElementById('remoteVideo');
            if (!overlay) {
                overlay = new OverlayController(video, "http://localhost:8080/api/v1", document.getElementById('token').value);
                overlay.setWeaponAlertCallback((p) => {
                    console.warn("WEAPON DETECTED:", p);
                    updateStatus("⚠️ WEAPON DETECTED!");
                });
            }

            if (overlayEnabled) {
                overlay.disable();
                overlayEnabled = false;
                document.getElementById('btnOverlay').innerText = "Enable Overlay";
            } else {
                // Mock session ID if needed, or rely on active variables
                if (!sessionID) {
                    alert("Start video first to get Session ID");
                    return;
                }
                overlay.token = document.getElementById('token').value; // Refresh token
                // Use cameraID as the ID (since it's 1:1 in this test)
                overlay.enable(sessionID, cameraID, { weapon: weaponEnabled });
                overlayEnabled = true;
                document.getElementById('btnOverlay').innerText = "Disable Overlay";
            }
        }

        function toggleWeapon() {
            weaponEnabled = !weaponEnabled;
            const btn = document.getElementById('btnWeapon');
            btn.innerText = weaponEnabled ? "Weapon AI: ON" : "Weapon AI: OFF";
            btn.style.background = weaponEnabled ? "#dc3545" : "#007bff";

            if (overlay) {
                overlay.weaponEnabled = weaponEnabled;
                // If currently running, re-enable to update params effectively or let poll cycle handle it
            }
        }

        // Check Server Version on Load (Safeguard 3)
        async function checkHealth() {
            try {
                const res = await fetch('http://localhost:8080/api/v1/healthz');
                if (res.ok) {
                    const data = await res.json();
                    console.log("Server Health:", data);
                    updateStatus(`Ready. Server: ${data.go_version} / ${data.commit}`);
                }
            } catch (e) {
                console.warn("Health check failed:", e);
            }
        }
        checkHealth();

        let device;
        let transport;
        let cameraID;
        let tenantID;
        let sessionID;

        function getTenantIdFromToken(token) {
            try {
                // Simple Base64Url to Base64 conversion
                var base64Url = token.split('.')[1];
                var base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
                // Pad with '='
                while (base64.length % 4) {
                    base64 += '=';
                }

                // Decode and parse
                var jsonPayload = window.atob(base64);
                const payload = JSON.parse(jsonPayload);
                return payload.tenant_id;
            } catch (e) {
                console.error('Token parsing error:', e);
                // Fail-safe for development: return the known default tenant ID if parsing breaks
                return '00000000-0000-0000-0000-000000000001';
            }
        }

        function generateUUID() { // Simple UUID v4
            return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
                var r = Math.random() * 16 | 0, v = c == 'x' ? r : (r & 0x3 | 0x8);
                return v.toString(16);
            });
        }

        // --- Phase 3.4 Stabilization Fixes ---
        let startInFlight = false;
        const activeTimers = new Set();
        const ENABLE_AUTORETRY = false; // C: Explicitly disable auto-retry by default

        function registerTimer(id) {
            activeTimers.add(id);
            return id;
        }

        function clearAllTimers() {
            activeTimers.forEach(id => clearTimeout(id));
            activeTimers.clear();
        }

        async function startView() {
            if (startInFlight) { // A: Single in-flight guard
                console.warn('Start already in-progress, ignoring click.');
                return;
            }
            startInFlight = true;

            try {
                // 1. New Run ID
                activeRunId++;
                const currentRunId = activeRunId;
                console.log(`RUN ${currentRunId} start`);

                // 2. Hard Stop previous run
                await hardStopPlayback();
                clearAllTimers(); // D: Clean up previous timers

                const id = document.getElementById('cameraId').value.trim();
                let tokenInput = document.getElementById('token').value.trim();

                if (tokenInput.startsWith("Token: ")) tokenInput = tokenInput.replace("Token: ", "");
                if (tokenInput.startsWith("Bearer ")) tokenInput = tokenInput.replace("Bearer ", "");

                const token = tokenInput;
                cameraID = id;

                if (!id || !token) {
                    alert('Camera ID and Token required');
                    return;
                }

                if (token.split('.').length !== 3) {
                    alert('Invalid Token format (must be a JWT)');
                    return;
                }

                tenantID = getTenantIdFromToken(token);
                sessionID = generateUUID();
                console.log(`RUN ${currentRunId} Tenant: ${tenantID}, Session: ${sessionID}`);

                try {
                    updateStatus(`[Run ${currentRunId}] Joining room...`);
                    // 1. Join Room & Get Router Rtp Capabilities
                    const joinResp = await fetch(`http://localhost:8080/api/v1/sfu/rooms/${id}/join`, {
                        method: 'POST',
                        headers: {
                            'Authorization': `Bearer ${token}`,
                            'Content-Type': 'application/json'
                        },
                        body: JSON.stringify({ sessionId: sessionID }) // Fix 8: Send Session ID
                    });

                    if (currentRunId !== activeRunId) return; // Guard

                    if (joinResp.status === 429) {
                        throw new Error('Room is full (max 50 viewers)');
                    }

                    if (!joinResp.ok) {
                        // Structured Error Handling (Phase 3.4)
                        let errData = {};
                        try {
                            errData = await joinResp.json();
                            console.error("Join Failed (Structured):", errData);
                        } catch (jsonErr) {
                            console.error("Join Failed (Raw):", joinResp.statusText);
                        }

                        // Check for Fallback Hint
                        if (errData.fallback_hint) {
                            console.warn(`RUN ${currentRunId} WebRTC failed, falling back to HLS.`);
                            let hlsUrl = errData.fallback_url;
                            if (hlsUrl && hlsUrl.startsWith("/")) {
                                hlsUrl = "http://localhost:8081" + hlsUrl;
                            }
                            fallbackToHLS(cameraID, token, hlsUrl, currentRunId);
                            return;
                        }

                        throw new Error(`Join failed: ${joinResp.status} ${joinResp.statusText} [${errData.error_code || 'UNKNOWN'}]`);
                    }

                    const routerRtpCapabilities = await joinResp.json();

                    // 2. Load Mediasoup Device
                    device = new mediasoupClient.Device();
                    await device.load({ routerRtpCapabilities });
                    updateStatus('Device loaded.');

                    // --- Task A: Safe, Optional WebSocket Signaling ---
                    const ENABLE_WS = false; // A1: Disabled by default
                    let ws = null;
                    let wsOpen = false;

                    function safeWsSend(obj) { // A2: Safe wrapper
                        if (ENABLE_WS && ws && wsOpen) {
                            try {
                                ws.send(JSON.stringify(obj));
                                return true;
                            } catch (e) {
                                console.warn("WS Send failed:", e);
                                return false;
                            }
                        }
                        return false;
                    }

                    if (ENABLE_WS) {
                        updateStatus('Device loaded. Connecting WS...');
                        // A3: Remove JWT from URL. TODO: Implement short-lived ticket if WS is needed.
                        const wsUrl = `ws://localhost:8080/api/v1/sfu/ws`;

                        try {
                            ws = new WebSocket(wsUrl);
                            ws.onopen = () => {
                                console.log('WS state: open');
                                wsOpen = true;
                                safeWsSend({ type: 'connection-state', state: 'connecting' });
                            };
                            ws.onerror = (e) => {
                                console.log('WS failed; continuing with HTTP-only signaling'); // Task C
                                updateStatus('WS unavailable; using HTTP-only.');
                                wsOpen = false;
                            };
                            ws.onclose = () => {
                                console.log('WS state: closed');
                                wsOpen = false;
                                ws = null;
                            };
                        } catch (wsErr) {
                            console.log('WS Setup Exception; continuing with HTTP-only signaling', wsErr);
                        }
                    } else {
                        console.log('WS disabled; using HTTP-only signaling'); // Task A1
                    }

                    if (device.canProduce('video')) {
                        // console.warn('We can produce video, but this is a View-Only client.');
                    }


                    // 3b. Create Transport on Server
                    const transportResp = await fetch(`http://localhost:8080/api/v1/sfu/rooms/${id}/transports`, {
                        method: 'POST',
                        headers: { 'Authorization': `Bearer ${token}` }
                    });

                    if (!transportResp.ok) throw new Error('Create transport failed');
                    const transportInfo = await transportResp.json();

                    // Create client-side transport (only once!)
                    transport = device.createRecvTransport(transportInfo);

                    transport.on('connect', async ({ dtlsParameters }, callback, errback) => {
                        if (currentRunId !== activeRunId) return;
                        try {
                            await fetch(`http://localhost:8080/api/v1/sfu/transports/${transport.id}/connect`, {
                                method: 'POST',
                                headers: {
                                    'Authorization': `Bearer ${token}`,
                                    'Content-Type': 'application/json'
                                },
                                body: JSON.stringify({ dtlsParameters })
                            });
                            callback();
                        } catch (error) {
                            console.error("Transport connect failed:", error);
                            errback(error);
                            if (ENABLE_AUTORETRY) fallbackToHLS(cameraID, token, null, currentRunId);
                        }
                    });

                    transport.on('connectionstatechange', (state) => {
                        console.log('Transport State:', state);

                        // Safe signaling
                        safeWsSend({ type: 'connection-state', state });

                        if (state === 'failed' || state === 'disconnected') {
                            console.warn('Transport failed, switching to HLS...');
                            fallbackToHLS(cameraID, token);
                        }
                    });

                    // ICE Timeout Watchdog
                    const watchdogId = setTimeout(() => {
                        if (transport && (transport.connectionState !== 'connected' && transport.connectionState !== 'completed')) {
                            console.warn('ICE Timeout (5s).');
                            if (currentRunId === activeRunId && ENABLE_AUTORETRY) fallbackToHLS(cameraID, token, null, currentRunId);
                        }
                    }, 5000);
                    registerTimer(watchdogId);

                    // 5. Consume Video Stream
                    const consumeUrl = `http://localhost:8080/api/v1/sfu/rooms/${id}/transports/${transport.id}/consume`;
                    console.log("Consuming from:", consumeUrl);
                    const consumeResp = await fetch(consumeUrl, {
                        method: 'POST',
                        headers: {
                            'Authorization': `Bearer ${token}`,
                            'Content-Type': 'application/json'
                        },
                        body: JSON.stringify({ rtpCapabilities: device.rtpCapabilities })
                    });

                    if (currentRunId !== activeRunId) return; // Guard

                    if (!consumeResp.ok) {
                        const errText = await consumeResp.text();
                        throw new Error(`Consume failed: ${consumeResp.status} ${consumeResp.statusText} - ${errText}`);
                    }
                    const consumerInfo = await consumeResp.json();

                    const consumer = await transport.consume({
                        id: consumerInfo.id,
                        producerId: consumerInfo.producerId,
                        kind: consumerInfo.kind,
                        rtpParameters: consumerInfo.rtpParameters,
                        // Fix: Increase jitter buffer to prevent freezing
                        appData: {
                            jitterBufferTarget: 300,  // 300ms buffer for network jitter
                            jitterBufferMaxPackets: 200
                        }
                    });

                    console.log(`RUN ${currentRunId} trackCount=1`);

                    // 5. Play Video
                    const video = document.getElementById('remoteVideo');
                    video.srcObject = new MediaStream([consumer.track]);

                    try {
                        await video.play();
                        updateStatus(`[Run ${currentRunId}] Streaming live via WebRTC. SUCCESS.`);
                        console.log(`RUN ${currentRunId} mode=webrtc SUCCESS`);

                        // E: Success Definition - Cancel all fallbacks/timers (except quality monitor)
                        clearAllTimers();

                        // Task D: Client Diagnostics (Quality Monitor)
                        let qualityCheckCount = 0;
                        const qualityTimerId = setInterval(() => {
                            if (currentRunId !== activeRunId) { clearInterval(qualityTimerId); return; }

                            const vWidth = video.videoWidth;
                            const vHeight = video.videoHeight;
                            // webkitVideoDecodedByteCount is non-standard but useful, 
                            // or use moz/std alternatives if available. video.getVideoPlaybackQuality() is standard.
                            let frames = 0;
                            if (video.getVideoPlaybackQuality) {
                                frames = video.getVideoPlaybackQuality().totalVideoFrames;
                            } else if (video.webkitDecodedFrameCount) {
                                frames = video.webkitDecodedFrameCount;
                            }

                            console.log(`[Run ${currentRunId}] Stats: ${vWidth}x${vHeight}, frames=${frames}`);

                            // If after 3 seconds (3 checks) we have 0 frames or 0x0 resolution
                            if (qualityCheckCount < 3) {
                                qualityCheckCount++;
                            } else {
                                if ((vWidth === 0 || vHeight === 0) || frames === 0) {
                                    console.warn(`[Run ${currentRunId}] Black screen detected (0x0 or 0 frames). Fallback to HLS.`);
                                    clearInterval(qualityTimerId);
                                    if (currentRunId === activeRunId) fallbackToHLS(cameraID, token, null, currentRunId);
                                } else {
                                    // Quality good, stop checking aggressively or reduce frequency? 
                                    // Let's keep checking periodically or just stop after initial success.
                                    // Requirement: "If after 3 seconds...". So once passed, we are good.
                                    clearInterval(qualityTimerId);
                                }
                            }
                        }, 1000);
                        registerTimer(qualityTimerId); // Ensure it's cleared on stop

                    } catch (playErr) {
                        if (playErr.name !== 'AbortError') {
                            console.error("Play error:", playErr);
                        }
                    }

                } catch (e) {
                    if (currentRunId !== activeRunId) return;
                    console.error(`RUN ${currentRunId} Error:`, e);
                    updateStatus('Error: ' + e.message + (ENABLE_AUTORETRY ? '. Retrying...' : '.'));
                    if (ENABLE_AUTORETRY) fallbackToHLS(cameraID, token, null, currentRunId);
                }
            } finally {
                startInFlight = false; // Release guard
            }
        }



        async function fallbackToHLS(cameraID, token, overrideUrl, runId) {
            // Guard: If this fallback request belongs to an old run, ignore it.
            if (runId !== activeRunId || (runId === undefined && activeRunId > 0)) {
                console.log(`Ignore stale HLS fallback req (runId=${runId} vs active=${activeRunId})`);
                return;
            }

            console.log(`RUN ${runId} switching webrtc->hls`);

            // Clean up WebRTC first
            if (transport) {
                transport.close();
                transport = null;
            }

            // Re-hard stop not strictly needed if we manage hlsInstance, 
            // but ensuring video element is reset is good.
            const video = document.getElementById('remoteVideo');
            video.srcObject = null;

            updateStatus(`[Run ${runId}] Streaming live (HLS Fallback).`);
            console.log(`RUN ${runId} mode=hls`);

            let sessionID;
            let hlsUrl = overrideUrl;
            let queryParams = "";

            const tenantID = getTenantIdFromToken(token);
            const secret = "dev-hls-secret";
            const exp = Math.floor(Date.now() / 1000) + (60 * 60); // 1 hour
            const kid = "v1";

            // If we don't have an override URL, we need to find the session ID
            if (!hlsUrl) {
                try {
                    const sessionRes = await fetch(`http://localhost:8081/hls/session/${cameraID}`);
                    if (!sessionRes.ok) throw new Error("No active HLS session");
                    const sessionData = await sessionRes.json();
                    sessionID = sessionData.session_id;
                    console.log("Session ID detected:", sessionID);
                } catch (e) {
                    console.error("HLS Session Error:", e);
                    updateStatus("HLS Failed: No active session (and no fallback URL provided)");
                    return;
                }

                // Generate Sig for manually constructed URL
                const canonical = `hls|${cameraID}|${sessionID}|${exp}`;

                const encoder = new TextEncoder();
                const keyData = encoder.encode(secret);
                const msgData = encoder.encode(canonical);

                const cryptoKey = await crypto.subtle.importKey(
                    "raw", keyData, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]
                );
                const signature = await crypto.subtle.sign("HMAC", cryptoKey, msgData);
                const sigHex = Array.from(new Uint8Array(signature))
                    .map(b => b.toString(16).padStart(2, "0"))
                    .join("");

                queryParams = `sub=${cameraID}&sid=${sessionID}&exp=${exp}&scope=hls&kid=${kid}&sig=${sigHex}`;
                hlsUrl = `http://localhost:8081/hls/live/${tenantID}/${cameraID}/${sessionID}/playlist.m3u8?${queryParams}`;
            } else {
                // If override URL is provided, we still need to append auth if it's missing the signature.
                // But EnsureHlsSession doesn't sign the URL.
                // So we MUST sign it here.
                // Extact SessionID from URL if possible, or just fail if we can't sign.
                // URL format: .../live/{tid}/{cid}/{sid}/playlist.m3u8
                const parts = hlsUrl.split('/');
                // ... tid, cid, sid, playlist.m3u8
                // Assuming standard structure.
                if (parts.length >= 4) {
                    sessionID = parts[parts.length - 2];

                    // Sign it
                    const canonical = `hls|${cameraID}|${sessionID}|${exp}`;
                    const encoder = new TextEncoder();
                    const keyData = encoder.encode(secret);
                    const msgData = encoder.encode(canonical);

                    const cryptoKey = await crypto.subtle.importKey(
                        "raw", keyData, { name: "HMAC", hash: "SHA-256" }, false, ["sign"]
                    );
                    const signature = await crypto.subtle.sign("HMAC", cryptoKey, msgData);
                    const sigHex = Array.from(new Uint8Array(signature))
                        .map(b => b.toString(16).padStart(2, "0"))
                        .join("");

                    queryParams = `sub=${cameraID}&sid=${sessionID}&exp=${exp}&scope=hls&kid=${kid}&sig=${sigHex}`;

                    if (hlsUrl.includes('?')) {
                        hlsUrl += '&' + queryParams;
                    } else {
                        hlsUrl += '?' + queryParams;
                    }
                }
            }

            console.log("Loading HLS URL:", hlsUrl);

            if (Hls.isSupported()) {
                if (hlsInstance) {
                    hlsInstance.destroy();
                }
                const hls = new Hls({
                    // CRITICAL: Propagate token to segment requests
                    xhrSetup: function (xhr, url) {
                        try {
                            xhr.setRequestHeader("Authorization", `Bearer ${token}`);
                        } catch (e) {/* ignore invalid state */ }

                        // Append params if segment URL needs them and logic requires
                        if (url.includes('.mp4') && !url.includes('sig=')) {
                            const separator = url.includes('?') ? '&' : '?';
                            xhr.open('GET', url + separator + queryParams);
                        }
                    }
                });
                hlsInstance = hls;
                hls.loadSource(hlsUrl);
                hls.attachMedia(video);
                hls.on(Hls.Events.MANIFEST_PARSED, function () {
                    if (runId === activeRunId) video.play().catch(() => { });
                });
                hls.on(Hls.Events.ERROR, function (event, data) {
                    console.error("HLS Fatal Error:", data);
                    if (data.fatal) {
                        switch (data.type) {
                            case Hls.ErrorTypes.NETWORK_ERROR:
                                hls.startLoad();
                                break;
                            case Hls.ErrorTypes.MEDIA_ERROR:
                                hls.recoverMediaError();
                                break;
                            default:
                                hls.destroy();
                                break;
                        }
                    }
                });
            } else if (video.canPlayType('application/vnd.apple.mpegurl')) {
                video.src = hlsUrl;
                video.addEventListener('loadedmetadata', function () {
                    if (runId === activeRunId) video.play().catch(() => { });
                });
            }
        }
        // --- Phase 3.4 State Machine ---
        let activeRunId = 0;
        let hlsInstance = null;

        async function hardStopPlayback() {
            console.log(`[Playback] Hard Stop triggered.`);
            const video = document.getElementById('remoteVideo');

            // 1. Pause and Clear
            video.pause();
            video.srcObject = null;
            video.src = "";
            video.load(); // Reset element

            // 2. Destroy HLS
            if (hlsInstance) {
                hlsInstance.destroy();
                hlsInstance = null;
                console.log(`[Playback] HLS destroyed.`);
            }

            // 3. Close WebRTC Transport and Producers (Client-side)
            if (transport) {
                transport.close();
                transport = null;
                console.log(`[Playback] WebRTC Transport closed.`);
            }

            // 4. Leave Room (Best effort, fire and forget)
            const token = document.getElementById('token').value;
            if (cameraID && token) {
                fetch(`http://localhost:8080/api/v1/sfu/sessions/${cameraID}/leave`, {
                    method: 'POST',
                    headers: { 'Authorization': `Bearer ${token}` }
                }).catch(() => { });
            }
        }

        async function stopView() {
            activeRunId++; // invalidate any running start
            console.log(`RUN ${activeRunId} stop`);
            await hardStopPlayback();
            updateStatus('Stopped.');
        }

        function updateStatus(msg) {
            document.getElementById('status').innerText = msg;
        }
