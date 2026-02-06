/**
 * GridController
 * Manages multi-camera grid layout, lazy loading, and concurrency limits.
 */
class GridController {
    constructor(config) {
        this.config = {
            maxActiveTiles: 16,
            maxConcurrentStarts: 4,
            startDebounceMs: 300,
            ...config
        };

        this.tiles = new Map(); // id -> { controller, element, state, queueItem }
        this.startQueue = []; // Array of ids waiting to start
        this.activeStarts = 0; // Count of currently "starting" WebRTC sessions

        // IntersectionObserver for visibility
        this.observer = new IntersectionObserver(this.handleIntersection.bind(this), {
            root: null, // Viewport
            rootMargin: '0px',
            threshold: 0.25 // Start only when 25% visible
        });

        // Stop observer (immediate)
        this.stopObserver = new IntersectionObserver(this.handleStopIntersection.bind(this), {
            threshold: 0.0 // trigger when fully off screen
        });

        this.startTimers = new Map(); // Debounce timers
    }

    /**
     * Registers a tile element for management
     * @param {string} id - Camera ID / Tile ID
     * @param {HTMLElement} element - Container element
     * @param {PlayerController} playerController - Instance of PlayerController
     */
    registerTile(id, element, playerController) {
        if (this.tiles.has(id)) return;

        this.tiles.set(id, {
            id,
            element,
            controller: playerController,
            state: 'IDLE'
        });

        // Start observing
        this.observer.observe(element);
        this.stopObserver.observe(element);
    }

    unregisterTile(id) {
        const tile = this.tiles.get(id);
        if (!tile) return;

        this.observer.unobserve(tile.element);
        this.stopObserver.unobserve(tile.element);

        this.stopTile(id);
        this.tiles.delete(id);
    }

    /**
     * Handles Intersection changes (Visibility)
     */
    handleIntersection(entries) {
        entries.forEach(entry => {
            const id = entry.target.dataset.tileId; // Assume data-tile-id set
            if (!id) return;

            if (entry.isIntersecting && entry.intersectionRatio >= 0.25) {
                // Enter Viewport -> Schedule Start
                this.scheduleStart(id);
            }
        });
    }

    handleStopIntersection(entries) {
        entries.forEach(entry => {
            const id = entry.target.dataset.tileId;
            if (!id) return;

            if (!entry.isIntersecting) {
                // Exit Viewport -> Stop Immediately
                this.cancelStart(id);
                this.stopTile(id);
            }
        });
    }

    /**
     * Schedules a tile start with debounce
     */
    scheduleStart(id) {
        if (this.startTimers.has(id)) return; // Already scheduled

        const timer = setTimeout(() => {
            this.startTimers.delete(id);
            this.queueStart(id);
        }, this.config.startDebounceMs);

        this.startTimers.set(id, timer);
    }

    cancelStart(id) {
        if (this.startTimers.has(id)) {
            clearTimeout(this.startTimers.get(id));
            this.startTimers.delete(id);
        }
        // Remove from queue if pending
        this.startQueue = this.startQueue.filter(itemId => itemId !== id);
    }

    /**
     * Adds to FIFO queue and processes
     */
    queueStart(id) {
        const tile = this.tiles.get(id);
        if (!tile || tile.state !== 'IDLE') return;

        // Add to queue if not present
        if (!this.startQueue.includes(id)) {
            this.startQueue.push(id);
        }

        this.processQueue();
    }

    /**
     * Process start queue respecting concurrency caps
     */
    processQueue() {
        if (this.activeStarts >= this.config.maxConcurrentStarts) return;
        if (this.startQueue.length === 0) return;

        const id = this.startQueue.shift();
        this.startTile(id);
    }

    /**
     * Actually starts the tile stream
     */
    async startTile(id) {
        const tile = this.tiles.get(id);
        if (!tile) return;

        this.activeStarts++;
        tile.state = 'STARTING';

        try {
            // Adaptive: Grid Mode always requests 'sub'
            const quality = 'sub';
            const viewMode = 'grid';

            // We wrap the player controller's start logic to include these params if supported
            // Assuming PlayerController exposed a `start(viewMode, quality)` method or config update
            // For Phase 3.7 we might need to patch PlayerController or pass options here.

            // NOTE: PlayerController.startLiveSession() needs to accept these.
            // We assume we updated PlayerController or will update it.
            // Let's pass an options object.
            await tile.controller.startLiveSession(id, { viewMode, quality });

            tile.state = 'PLAYING';
        } catch (err) {
            console.error(`Tile ${id} failed to start:`, err);
            tile.state = 'ERROR';
            // If limit exceeded, maybe UI badge?
        } finally {
            this.activeStarts--;
            this.processQueue(); // Pick next
        }
    }

    async stopTile(id) {
        const tile = this.tiles.get(id);
        if (!tile) return;

        if (tile.state !== 'IDLE') {
            await tile.controller.stop(); // Clean stop (teardown PC, timers)
            tile.state = 'IDLE';
        }
    }

    /**
     * Switch a tile to fullscreen (Maximize)
     */
    async maximizeTile(id) {
        const tile = this.tiles.get(id);
        if (!tile) return;

        // 1. Clean Stop (Grid Mode)
        this.cancelStart(id); // Cancel debounce/queue
        await this.stopTile(id);

        // 2. Start Fullscreen (Main Quality)
        // We assume UI handles the DOM expansion
        // Here we just restart the stream
        try {
            await tile.controller.startLiveSession(id, {
                viewMode: 'fullscreen',
                quality: 'main'
            });
            tile.state = 'FULLSCREEN';
        } catch (e) {
            console.error('Maximize failed', e);
        }
    }

    /**
     * Return to grid from fullscreen
     */
    async restoreGrid(id) {
        const tile = this.tiles.get(id);
        if (!tile) return;

        // 1. Clean Stop (Fullscreen Mode)
        await tile.controller.stop();
        tile.state = 'IDLE';

        // 2. Let Observer handle restart (lazy)
        // Or force immediate check:
        // this.scheduleStart(id);
    }
}
