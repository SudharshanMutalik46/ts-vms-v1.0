/**
 * Techno Support VMS - Live Player Controller (Phase 3.6)
 * Handles dual-path WebRTC -> HLS graceful fallback.
 */

const STATE = {
    IDLE: 'IDLE',
    STARTING_WEBRTC: 'STARTING_WEBRTC',
    PLAYING_WEBRTC: 'PLAYING_WEBRTC',
    FALLBACK_STARTING_HLS: 'FALLBACK_STARTING_HLS',
    PLAYING_HLS: 'PLAYING_HLS',
    RETRYING_WEBRTC: 'RETRYING_WEBRTC',
    STOPPED: 'STOPPED'
};

const EVENTS = {
    WEBRTC_ATTEMPT: 'webrtc_attempt',
    WEBRTC_CONNECTED: 'webrtc_connected',
    WEBRTC_FIRST_FRAME: 'webrtc_first_frame', // ReadyState >= 2
    WEBRTC_FAILED: 'webrtc_failed',
    FALLBACK_TO_HLS: 'fallback_to_hls',
    HLS_PLAYING: 'hls_playing',
    RETRY_WEBRTC: 'retry_webrtc_clicked',
    SESSION_END: 'session_end'
};

const REASONS = {
    CONNECT_TIMEOUT: 'CONNECT_TIMEOUT',
    TRACK_TIMEOUT: 'TRACK_TIMEOUT',
    ICE_FAILED: 'ICE_FAILED',
    DTLS_FAILED: 'DTLS_FAILED',
    SFU_SIGNALING_FAILED: 'SFU_SIGNALING_FAILED',
    SFU_BUSY: 'SFU_BUSY',
    PERMISSION_DENIED: 'PERMISSION_DENIED',
    BROWSER_NOT_SUPPORTED: 'BROWSER_NOT_SUPPORTED',
    UNKNOWN: 'UNKNOWN'
};

class PlayerController {
    constructor(config) {
        this.config = config; // { videoElement, device, mediasoupClient, hlsCtor }
        this.state = STATE.IDLE;
        this.session = null; // API response
        this.pc = null; // RTCPeerConnection
        this.hls = null; // hls.js instance
        this.autoRetryCount = 0;
        this.retryTimer = null;

        // Timeouts
        this.connectTimer = null;
        this.trackTimer = null;

        // Telemetry
        this.startRequestTime = 0;
        this.fallbackTime = 0;

        // Bindings
        this.video = this.config.videoElement;
    }

    async startLiveSession(cameraID, options = {}) {
        if (this.state !== STATE.IDLE && this.state !== STATE.STOPPED) {
            console.warn("Player already running, stop first.");
            return;
        }

        this.startRequestTime = Date.now();
        await this._loadSession(cameraID, options);
    }

    stop() {
        this._transition(STATE.STOPPED);
        this._cleanupWebRTC();
        this._cleanupHLS();
        this._sendTelemetry(EVENTS.SESSION_END);
        this.session = null;
    }

    async retryWebRTC() {
        this._sendTelemetry(EVENTS.RETRY_WEBRTC);
        this._transition(STATE.RETRYING_WEBRTC);
        this._cleanupHLS(); // Stop HLS before retrying
        this.autoRetryCount = 0; // Reset auto retries on manual action
        await this._attemptWebRTC();
    }

    // --- State Machine Internals ---

    async _loadSession(cameraID, options) {
        try {
            // API Call: POST /api/v1/cameras/{id}/live/start
            // Phase 3.7: Send view_mode and quality in body
            const body = {
                view_mode: options.viewMode || 'fullscreen',
                quality: options.quality || 'auto'
            };

            const resp = await fetch(`/api/v1/cameras/${cameraID}/live/start`, {
                method: 'POST',
                headers: {
                    'Authorization': `Bearer ${this.config.token}`,
                    'Content-Type': 'application/json'
                },
                body: JSON.stringify(body)
            });

            if (!resp.ok) {
                if (resp.status === 429) {
                    // Limit Exceeded
                    console.warn("Live Limit Exceeded");
                    // We could expose a specific state or event for UI
                    this._handleError(REASONS.SFU_BUSY); // Use BUSY for limit
                    return;
                }
                if (resp.status === 403 || resp.status === 401) {
                    this._handleError(REASONS.PERMISSION_DENIED);
                } else {
                    this._handleError(REASONS.UNKNOWN);
                }
                return;
            }

            this.session = await resp.json();
            await this._attemptWebRTC();

        } catch (e) {
            console.error("Start API failed", e);
            this._handleError(REASONS.SFU_SIGNALING_FAILED);
        }
    }

    async _attemptWebRTC() {
        this._transition(STATE.STARTING_WEBRTC);
        this._sendTelemetry(EVENTS.WEBRTC_ATTEMPT);

        // Set Connection Deadline
        const deadline = this.session.fallback_policy.webrtc_connect_timeout_ms || 5000;
        this.connectTimer = setTimeout(() => {
            if (this.state === STATE.STARTING_WEBRTC) {
                console.warn("WebRTC Connect Timeout");
                this._triggerFallback(REASONS.CONNECT_TIMEOUT);
            }
        }, deadline);

        try {
            await this._initWebRTC(this.session.webrtc);
        } catch (e) {
            console.error("WebRTC Init Failed", e);
            this._cleanupWebRTC();
            this._triggerFallback(this._mapErrorToReason(e));
        }
    }

    async _initWebRTC(config) {
        const device = new this.config.mediasoupClient.Device();

        // 1. Get Router Capabilities
        const capsResp = await this._signalingFetch(`${config.sfu_url}/rooms/${config.room_id}/rtp-capabilities`);
        await device.load({ routerRtpCapabilities: capsResp });

        // 2. Create Transport
        const transportInfo = await this._signalingFetch(`${config.sfu_url}/rooms/${config.room_id}/transports`, 'POST', {
            forceTcp: false, rtpCapabilities: device.rtpCapabilities
        });

        this.pc = device.createRecvTransport(transportInfo);

        this.pc.on('connect', async ({ dtlsParameters }, callback, errback) => {
            try {
                await this._signalingFetch(`${config.sfu_url}/transports/${transportInfo.id}/connect`, 'POST', { dtlsParameters });
                callback();
            } catch (e) {
                errback(e);
            }
        });

        this.pc.on('connectionstatechange', (state) => {
            console.log("ICE State:", state);
            if (state === 'connected') {
                this._sendTelemetry(EVENTS.WEBRTC_CONNECTED);
                // Connected! Now wait for Track
                this._startTrackTimer();
                clearTimeout(this.connectTimer); // Clear connect deadline
            }
            if (state === 'failed') {
                this._triggerFallback(REASONS.ICE_FAILED);
            }
            if (state === 'disconnected') {
                // Grace period for disconnected
                setTimeout(() => {
                    if (this.pc && this.pc.connectionState === 'disconnected') {
                        this._triggerFallback(REASONS.ICE_FAILED);
                    }
                }, 1500);
            }
        });

        // 3. Consume
        const consumeResp = await this._signalingFetch(`${config.sfu_url}/rooms/${config.room_id}/transports/${transportInfo.id}/consume`, 'POST', {
            rtpCapabilities: device.rtpCapabilities
        });

        const consumer = await this.pc.consume({
            id: consumeResp.id,
            producerId: consumeResp.producerId,
            kind: consumeResp.kind,
            rtpParameters: consumeResp.rtpParameters,
            appData: { jitterBufferTarget: 300 } // Phase 3.5 fix
        });

        const stream = new MediaStream([consumer.track]);
        this.video.srcObject = stream;

        // Monitor First Frame
        const onPlaying = () => {
            if (this.video.readyState >= 2) {
                this._transition(STATE.PLAYING_WEBRTC);
                const ttff = Date.now() - this.startRequestTime;
                this._sendTelemetry(EVENTS.WEBRTC_FIRST_FRAME, { ttff_ms: ttff });
                clearTimeout(this.trackTimer);
                this.video.removeEventListener('timeupdate', onPlaying);
            }
        };
        this.video.addEventListener('timeupdate', onPlaying);

        // Start playback
        try {
            await this.video.play();
        } catch (e) { /* Autoplay block? */ }
    }

    _startTrackTimer() {
        const deadline = this.session.fallback_policy.webrtc_track_timeout_ms || 2000;
        this.trackTimer = setTimeout(() => {
            if (this.state === STATE.STARTING_WEBRTC) { // Still starting, meaning no frame yet
                console.warn("WebRTC Track Timeout (Connected but no data)");
                this._triggerFallback(REASONS.TRACK_TIMEOUT);
            }
        }, deadline);
    }

    // --- Fallback Logic ---

    _triggerFallback(reason) {
        if (this.state === STATE.PLAYING_HLS || this.state === STATE.FALLBACK_STARTING_HLS || this.state === STATE.STOPPED) return;

        console.warn(`Fallback triggered: ${reason}`);
        this._sendTelemetry(EVENTS.WEBRTC_FAILED, { reason });

        this._cleanupWebRTC();
        this._startHLS(reason);
    }

    _startHLS(reason) {
        this.fallbackTime = Date.now();
        this._transition(STATE.FALLBACK_STARTING_HLS);
        this._sendTelemetry(EVENTS.FALLBACK_TO_HLS, { reason });

        const url = this.session.hls.playlist_url;

        // Safari Native HLS Check
        if (this.video.canPlayType('application/vnd.apple.mpegurl')) {
            this.video.src = url;
            this.video.onloadedmetadata = () => {
                this.video.play();
                this._transition(STATE.PLAYING_HLS);
                this._sendTelemetry(EVENTS.HLS_PLAYING, { ttff_ms: Date.now() - this.fallbackTime });
            };
        }
        // HLS.js (MSE) Check
        else if (this.config.hlsCtor && this.config.hlsCtor.isSupported()) {
            this.hls = new this.config.hlsCtor();
            this.hls.loadSource(url);
            this.hls.attachMedia(this.video);
            this.hls.on(this.config.hlsCtor.Events.MANIFEST_PARSED, () => {
                this.video.play();
            });
            this.hls.on(this.config.hlsCtor.Events.FRAG_PARSED, (event, data) => {
                if (this.state !== STATE.PLAYING_HLS) {
                    this._transition(STATE.PLAYING_HLS);
                    this._sendTelemetry(EVENTS.HLS_PLAYING, { ttff_ms: Date.now() - this.fallbackTime });
                }
            });
        } else {
            this._handleError(REASONS.BROWSER_NOT_SUPPORTED);
            return;
        }

        // Schedule Auto-Retry
        if (this.autoRetryCount < this.session.fallback_policy.max_auto_retries) {
            this.autoRetryCount++;
            const interval = this.session.fallback_policy.retry_backoff_ms[0] || 30000;
            console.log(`Scheduling auto-retry #${this.autoRetryCount} in ${interval}ms`);
            this.retryTimer = setTimeout(() => this.retryWebRTC(), interval);
        }
    }

    // --- Helpers ---

    _cleanupWebRTC() {
        if (this.pc) {
            this.pc.close();
            this.pc = null;
        }
        if (this.video.srcObject) {
            this.video.srcObject.getTracks().forEach(t => t.stop());
            this.video.srcObject = null;
        }
        clearTimeout(this.connectTimer);
        clearTimeout(this.trackTimer);
    }

    _cleanupHLS() {
        if (this.hls) {
            this.hls.destroy();
            this.hls = null;
        }
        if (this.video.src && this.video.src.includes(".m3u8")) {
            this.video.removeAttribute('src'); // For Safari
            this.video.load();
        }
        clearTimeout(this.retryTimer);
    }

    _transition(newState) {
        console.log(`[Player] ${this.state} -> ${newState}`);
        this.state = newState;
        if (this.config.onStateChange) this.config.onStateChange(newState);
    }

    async _sendTelemetry(type, meta = {}) {
        if (!this.session) return;
        try {
            await fetch(this.session.telemetry_policy.client_event_endpoint, {
                method: 'POST',
                headers: { 'Content-Type': 'application/json', 'Authorization': `Bearer ${this.config.token}` },
                body: JSON.stringify({
                    viewer_session_id: this.session.viewer_session_id,
                    camera_id: this.session.webrtc.room_id,
                    event_type: type,
                    reason_code: meta.reason,
                    mode: (this.state.includes("HLS") ? "hls" : "webrtc"),
                    ttff_ms: meta.ttff_ms,
                    ts_unix_ms: Date.now()
                })
            });
        } catch (e) { /* Fire and forget */ }
    }

    async _signalingFetch(url, method = 'GET', body = null) {
        const opts = {
            method,
            headers: { 'Authorization': `Bearer ${this.config.token}` }
        };
        if (body) {
            opts.headers['Content-Type'] = 'application/json';
            opts.body = JSON.stringify(body);
        }
        const res = await fetch(url, opts);
        if (!res.ok) throw new Error(`Signaling failed: ${res.status}`);
        return res.json();
    }

    _mapErrorToReason(e) {
        // Simple heuristic mapping
        const s = e.toString().toLowerCase();
        if (s.includes("ice") || s.includes("connection")) return REASONS.ICE_FAILED;
        if (s.includes("dtls")) return REASONS.DTLS_FAILED;
        if (s.includes("fetch") || s.includes("network")) return REASONS.SFU_SIGNALING_FAILED;
        return REASONS.UNKNOWN;
    }

    _handleError(reason) {
        console.error("Fatal Player Error:", reason);
        this._sendTelemetry(EVENTS.WEBRTC_FAILED, { reason }); // Or specific fatal event?
        // Fallback or Stop? depends, usually Stop if HLS also failed.
    }
}

// Export for module use or global
window.TechnoPlayer = PlayerController;
