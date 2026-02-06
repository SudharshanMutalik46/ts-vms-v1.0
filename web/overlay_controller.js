/**
 * Phase 3.8 Overlay Controller
 * Renders AI detection bounding boxes over video elements
 * Supports both WebRTC and HLS playback modes
 */
class OverlayController {
    // Color mapping per Phase 3.8 spec
    static COLORS = {
        // Basic (10 classes)
        person: '#00FF00',     // Green
        car: '#0088FF',        // Blue
        truck: '#0088FF',      // Blue
        bus: '#0088FF',        // Blue
        motorcycle: '#0088FF', // Blue
        bicycle: '#0088FF',    // Blue
        cat: '#FFFF00',        // Yellow
        dog: '#FFFF00',        // Yellow
        bird: '#FFFF00',       // Yellow
        bag: '#FF8800',        // Orange
        // Weapon (3 classes)
        handgun: '#FF0000',    // Red
        rifle: '#FF0000',      // Red
        knife: '#FF0000',      // Red
    };

    static WEAPON_LABELS = ['handgun', 'rifle', 'knife'];

    constructor(videoParam, apiBase, token) {
        if (typeof videoParam === 'string') {
            this.video = document.getElementById(videoParam);
        } else {
            this.video = videoParam;
        }

        this.apiBase = apiBase || '/api/v1';
        this.token = token;

        this.canvas = null;
        this.ctx = null;
        this.enabled = false;
        this.weaponEnabled = false;

        // State
        this.sessionID = null;
        this.cameraID = null;
        this.pollTimer = null;
        this.pollInterval = 1000;
        this.isPolling = false;

        // Throttling: max 10 updates/sec
        this.lastRenderTime = 0;
        this.minRenderInterval = 100; // 100ms = 10fps max

        // Weapon alert callback
        this.onWeaponAlert = null;

        // AI unavailable state
        this.consecutiveErrors = 0;
        this.maxConsecutiveErrors = 5;
    }

    // Called when user clicks "Toggle Overlay"
    async toggle(sessionID, cameraID) {
        if (this.enabled) {
            await this.disable();
        } else {
            await this.enable(sessionID, cameraID);
        }
        return this.enabled;
    }

    async enable(sessionID, cameraID, options = {}) {
        if (this.enabled) return;

        console.log(`[Overlay] Enabling for Sess=${sessionID} Cam=${cameraID}`);
        this.sessionID = sessionID;
        this.cameraID = cameraID;
        this.weaponEnabled = options.weapon || false;

        try {
            // 1. Tell Backend to Enable (Start tracking active view)
            const resp = await fetch(`${this.apiBase}/live/${sessionID}/overlay/enable`, {
                method: 'POST',
                headers: { 'Authorization': `Bearer ${this.token}` }
            });
            if (!resp.ok) throw new Error(`Backend Enable Failed: ${resp.status}`);

            // 2. Create Canvas
            this._createCanvas();

            // 3. Start Polling
            this.enabled = true;
            this.pollInterval = 1000;
            this.consecutiveErrors = 0;
            this._startPolling();

        } catch (e) {
            console.error("[Overlay] Enable failed", e);
            this._showAIUnavailable();
        }
    }

    async disable() {
        if (!this.enabled) return;

        console.log(`[Overlay] Disabling Sess=${this.sessionID}`);

        // 1. Stop Polling
        this.enabled = false;
        clearTimeout(this.pollTimer);
        this.isPolling = false;

        // 2. Remove Canvas
        this._destroyCanvas();

        // 3. Tell Backend to Disable
        if (this.sessionID) {
            try {
                await fetch(`${this.apiBase}/live/${this.sessionID}/overlay/disable`, {
                    method: 'POST',
                    headers: { 'Authorization': `Bearer ${this.token}` }
                });
            } catch (e) {
                console.warn("[Overlay] Backend disable failed", e);
            }
        }

        this.sessionID = null;
        this.cameraID = null;
    }

    _createCanvas() {
        if (this.canvas) return;

        // Wrap video in a positioning container if needed
        let container = this.video.parentElement;
        const style = window.getComputedStyle(container);

        // If parent is body or not suitable for absolute positioning, we might need a dedicated wrapper.
        // For simplicity and robustness, we ensure the parent is relative and the video is contained.
        if (style.position === 'static' || container === document.body) {
            const wrapper = document.createElement('div');
            wrapper.style.position = 'relative';
            wrapper.style.display = 'inline-block'; // Fit to content (video)
            wrapper.style.width = this.video.style.width || 'auto';
            wrapper.style.maxWidth = this.video.style.maxWidth || 'none';

            this.video.parentNode.insertBefore(wrapper, this.video);
            wrapper.appendChild(this.video);
            container = wrapper;
        } else if (style.position === 'static') {
            container.style.position = 'relative';
        }

        this.canvas = document.createElement('canvas');
        this.canvas.style.position = 'absolute';
        this.canvas.style.top = '0';
        this.canvas.style.left = '0';
        this.canvas.style.width = '100%';
        this.canvas.style.height = '100%';
        this.canvas.style.pointerEvents = 'none';
        this.canvas.style.zIndex = '10';

        // Initial size
        this.canvas.width = this.video.clientWidth || 640;
        this.canvas.height = this.video.clientHeight || 360;

        container.appendChild(this.canvas);
        this.ctx = this.canvas.getContext('2d');
    }

    _destroyCanvas() {
        if (this.canvas) {
            this.canvas.remove();
            this.canvas = null;
            this.ctx = null;
        }
    }

    _startPolling() {
        if (!this.enabled) return;

        const poll = async () => {
            if (!this.enabled) return;
            this.isPolling = true;

            try {
                // Poll basic stream
                await this._pollStream('basic');

                // Poll weapon stream if enabled
                if (this.weaponEnabled) {
                    await this._pollStream('weapon');
                }

                this.consecutiveErrors = 0;

            } catch (e) {
                console.warn("[Overlay] Poll error", e);
                this.consecutiveErrors++;
                if (this.consecutiveErrors >= this.maxConsecutiveErrors) {
                    this._showAIUnavailable();
                }
            } finally {
                this.isPolling = false;
                if (this.enabled) {
                    this.pollTimer = setTimeout(poll, this.pollInterval);
                }
            }
        };

        poll();
    }

    async _pollStream(stream) {
        const resp = await fetch(`${this.apiBase}/cameras/${this.cameraID}/detections/latest?stream=${stream}`, {
            headers: { 'Authorization': `Bearer ${this.token}` }
        });

        if (resp.status === 200) {
            const data = await resp.json();

            // Check for upgrade_required (weapon not licensed)
            if (data.upgrade_required) {
                this._showUpgradeMessage(data.feature);
                return;
            }

            this._render(data, stream);
            this.pollInterval = 1000;
        } else if (resp.status === 204) {
            // No content (stale), exponential backoff
            this.pollInterval = Math.min(this.pollInterval * 2, 4000);
            if (stream === 'basic') {
                this._clearCanvas();
            }
        }
    }

    _render(payload, stream) {
        if (!this.canvas || !this.ctx) return;

        // Throttle rendering to max 10fps
        const now = Date.now();
        if (now - this.lastRenderTime < this.minRenderInterval) {
            return;
        }
        this.lastRenderTime = now;

        // Check staleness (age_ms from server or compute from ts_unix_ms)
        const age = payload.age_ms || (now - payload.ts_unix_ms);
        if (age > 4000) {
            this._clearCanvas();
            return;
        }

        // Resize if needed
        if (this.canvas.width !== this.video.clientWidth || this.canvas.height !== this.video.clientHeight) {
            this.canvas.width = this.video.clientWidth || 640;
            this.canvas.height = this.video.clientHeight || 360;
        }

        const ctx = this.ctx;
        const w = this.canvas.width;
        const h = this.canvas.height;

        // Only clear for basic stream (weapon overlays on top)
        if (stream === 'basic') {
            ctx.clearRect(0, 0, w, h);
        }

        if (!payload.objects) return;

        // Calculate actual video display rect within the canvas (letterboxing helper)
        const rect = this._getVideoDisplayRect();

        ctx.lineWidth = 2;
        ctx.font = '14px sans-serif';

        let hasWeapon = false;

        payload.objects.forEach(obj => {
            // Map normalized coordinates (0-1) to the actual video rectangle
            const bx = rect.x + (obj.bbox.x * rect.width);
            const by = rect.y + (obj.bbox.y * rect.height);
            const bw = obj.bbox.w * rect.width;
            const bh = obj.bbox.h * rect.height;

            // Get color based on label
            const color = OverlayController.COLORS[obj.label] || '#FFFFFF';

            // Draw box
            ctx.strokeStyle = color;
            ctx.strokeRect(bx, by, bw, bh);

            // Draw label background
            const labelText = `${obj.label} (${Math.round(obj.confidence * 100)}%)`;
            const textMetrics = ctx.measureText(labelText);
            ctx.fillStyle = 'rgba(0,0,0,0.7)';
            ctx.fillRect(bx, by - 18, textMetrics.width + 6, 18);

            // Draw label text
            ctx.fillStyle = color;
            ctx.fillText(labelText, bx + 3, by - 5);

            // Check for weapon
            if (OverlayController.WEAPON_LABELS.includes(obj.label)) {
                hasWeapon = true;
            }
        });

        // Trigger weapon alert if detected
        if (hasWeapon && this.onWeaponAlert) {
            this.onWeaponAlert(payload);
        }
    }

    _clearCanvas() {
        if (this.canvas && this.ctx) {
            this.ctx.clearRect(0, 0, this.canvas.width, this.canvas.height);
        }
    }

    _showAIUnavailable() {
        if (!this.canvas || !this.ctx) return;

        const ctx = this.ctx;
        const w = this.canvas.width;

        ctx.fillStyle = 'rgba(0,0,0,0.5)';
        ctx.fillRect(w - 130, 5, 125, 25);

        ctx.fillStyle = '#FFaa00';
        ctx.font = '12px sans-serif';
        ctx.fillText('⚠ AI Unavailable', w - 125, 22);
    }

    _showUpgradeMessage(feature) {
        if (!this.canvas || !this.ctx) return;

        const ctx = this.ctx;
        const w = this.canvas.width;

        ctx.fillStyle = 'rgba(0,0,0,0.7)';
        ctx.fillRect(w - 180, 5, 175, 25);

        ctx.fillStyle = '#00AAFF';
        ctx.font = '12px sans-serif';
        ctx.fillText('🔒 Upgrade for Weapon AI', w - 175, 22);
    }

    // Calculate the actual display rectangle of the video content (accounting for object-fit: contain/cover)
    _getVideoDisplayRect() {
        const video = this.video;
        const canvas = this.canvas;

        const videoWidth = video.videoWidth || 640;
        const videoHeight = video.videoHeight || 360;
        const containerWidth = canvas.width;
        const containerHeight = canvas.height;

        const videoRatio = videoWidth / videoHeight;
        const containerRatio = containerWidth / containerHeight;

        let displayWidth, displayHeight, x, y;

        // Assuming object-fit: contain (standard for VMS)
        // If the user uses 'cover', we would flip the logic, but 'contain' is default for aspect-correct monitoring.
        if (containerRatio > videoRatio) {
            // Container is wider than video (pillarbox)
            displayHeight = containerHeight;
            displayWidth = containerHeight * videoRatio;
            x = (containerWidth - displayWidth) / 2;
            y = 0;
        } else {
            // Container is taller than video (letterbox)
            displayWidth = containerWidth;
            displayHeight = containerWidth / videoRatio;
            x = 0;
            y = (containerHeight - displayHeight) / 2;
        }

        return { x, y, width: displayWidth, height: displayHeight };
    }

    // Set callback for weapon alerts
    setWeaponAlertCallback(callback) {
        this.onWeaponAlert = callback;
    }
}

// Export
window.OverlayController = OverlayController;
