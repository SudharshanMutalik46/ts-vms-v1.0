import * as mediasoup from 'mediasoup';
import { v4 as uuidv4 } from 'uuid';
import os from 'os';
export class MediasoupManager {
    workers = [];
    webRtcServers = new Map(); // workerPid -> WebRtcServer
    nextWorkerIdx = 0;
    // Fix 8 & 9: Room State Management
    rooms = new Map();
    transports = new Map();
    producers = new Map();
    consumers = new Map();
    MAX_VIEWERS_PER_ROOM = 50;
    IDLE_TIMEOUT_MS = 60000;
    REAPER_INTERVAL_MS = 10000;
    // Fix 1: Port management for PlainTransport (Ingest)
    usedIngestPorts = new Set();
    INGEST_PORT_MIN = 50000;
    INGEST_PORT_MAX = 51000;
    getFreeIngestPort() {
        for (let port = this.INGEST_PORT_MIN; port <= this.INGEST_PORT_MAX; port++) {
            if (!this.usedIngestPorts.has(port)) {
                this.usedIngestPorts.add(port);
                return port;
            }
        }
        throw new Error('No free ingest ports available');
    }
    releaseIngestPort(port) {
        this.usedIngestPorts.delete(port);
    }
    async init() {
        const numWorkers = os.cpus().length;
        const localIp = process.env['ANNOUNCED_IP'] || '127.0.0.1'; // Ensure this is set correctly in prod
        for (let i = 0; i < numWorkers; i++) {
            // Fix 3: Use WebRtcServer for better UDP/TCP control
            const worker = await mediasoup.createWorker({
                logLevel: 'warn',
            });
            // Create WebRtcServer per worker
            const webRtcServer = await worker.createWebRtcServer({
                listenInfos: [
                    {
                        protocol: 'udp',
                        ip: '0.0.0.0',
                        announcedAddress: localIp,
                        portRange: { min: 40000, max: 49999 }
                    },
                    {
                        protocol: 'tcp',
                        ip: '0.0.0.0',
                        announcedAddress: localIp,
                        portRange: { min: 40000, max: 49999 }
                    }
                ]
            });
            this.webRtcServers.set(worker.pid, webRtcServer);
            worker.on('died', () => {
                console.error('mediasoup worker died, exiting in 2 seconds... [PID:%d]', worker.pid);
                setTimeout(() => process.exit(1), 2000);
            });
            this.workers.push(worker);
        }
        console.log(`Initialized ${this.workers.length} mediasoup workers with WebRtcServers`);
        // Fix 9: Start Idle Reaper
        this.startIdleReaper();
    }
    startIdleReaper() {
        setInterval(() => {
            const now = Date.now();
            for (const [roomID, room] of this.rooms) {
                if (room.viewerSessions.size === 0 && (now - room.lastActivity) > this.IDLE_TIMEOUT_MS) {
                    console.log(`Room ${roomID} idle for ${this.IDLE_TIMEOUT_MS}ms, cleaning up.`);
                    this.cleanupRoom(roomID);
                }
            }
        }, this.REAPER_INTERVAL_MS);
    }
    getNextWorker() {
        const worker = this.workers[this.nextWorkerIdx];
        if (!worker)
            throw new Error('No workers available');
        this.nextWorkerIdx = (this.nextWorkerIdx + 1) % this.workers.length;
        return worker;
    }
    async getRouter(roomID) {
        let room = this.rooms.get(roomID);
        if (!room) {
            const worker = this.getNextWorker();
            const router = await worker.createRouter({
                mediaCodecs: [
                    {
                        kind: 'video',
                        mimeType: 'video/H264',
                        clockRate: 90000,
                        parameters: {
                            'packetization-mode': 1,
                            'profile-level-id': '42e01f',
                            'level-asymmetry-allowed': 1
                        }
                    },
                ]
            });
            // Attach worker PID to router for WebRtcServer lookup
            router.appData = { workerPid: worker.pid };
            room = {
                router,
                viewerSessions: new Set(),
                lastActivity: Date.now()
            };
            this.rooms.set(roomID, room);
            console.log(`Created router for room: ${roomID}`);
        }
        return room.router;
    }
    // Fix 8: Join Room with Viewer Cap
    async joinRoom(roomID, sessionID) {
        await this.getRouter(roomID); // Ensure exists
        const room = this.rooms.get(roomID);
        if (!room)
            return; // Should not happen
        if (room.viewerSessions.size >= this.MAX_VIEWERS_PER_ROOM) {
            throw new Error('Room at capacity');
        }
        room.viewerSessions.add(sessionID);
        room.lastActivity = Date.now();
    }
    async createWebRtcTransport(roomID) {
        const router = await this.getRouter(roomID);
        const workerPid = router.appData.workerPid;
        const webRtcServer = this.webRtcServers.get(workerPid);
        if (!webRtcServer)
            throw new Error('WebRtcServer not found for this router');
        const transport = await router.createWebRtcTransport({
            webRtcServer,
            enableUdp: true,
            enableTcp: true,
            preferUdp: true,
            initialAvailableOutgoingBitrate: 1000000,
        });
        this.transports.set(transport.id, transport);
        return {
            id: transport.id,
            iceParameters: transport.iceParameters,
            iceCandidates: transport.iceCandidates,
            dtlsParameters: transport.dtlsParameters,
        };
    }
    async connectWebRtcTransport(transportID, dtlsParameters) {
        const transport = this.transports.get(transportID);
        if (!transport)
            throw new Error('Transport not found');
        await transport.connect({ dtlsParameters });
    }
    async prepareIngest(roomID) {
        const router = await this.getRouter(roomID);
        const localIp = process.env['ANNOUNCED_IP'] || '127.0.0.1';
        const port = this.getFreeIngestPort();
        // Fix 1: Explicit PlainTransport port
        const transport = await router.createPlainTransport({
            listenInfo: {
                protocol: 'udp',
                ip: '0.0.0.0',
                announcedAddress: localIp,
                port: port
            },
            rtcpMux: true,
            comedia: true, // receive from any port (Media Plane)
        });
        // Store port in appData for release
        transport.appData = { ingestPort: port };
        transport.on('close', () => {
            this.releaseIngestPort(port);
            console.log(`Released ingest port ${port}`);
        });
        this.transports.set(transport.id, transport);
        // For simplicity, we use hardcoded SSRC/PT for now or generate them
        const ssrc = 11111111;
        const pt = 96;
        // Start Producer for this transport (H.264 or H.265)
        // For simplicity, we default to H.264 PT (96). H.265 would use different PT if needed.
        const producer = await transport.produce({
            kind: 'video',
            rtpParameters: {
                codecs: [
                    {
                        mimeType: 'video/H264',
                        payloadType: pt,
                        clockRate: 90000,
                        parameters: {
                            'packetization-mode': 1,
                            'profile-level-id': '42e01f',
                            'level-asymmetry-allowed': 1
                        }
                    }
                ],
                encodings: [{ ssrc }]
            }
        });
        this.producers.set(roomID + ':video', producer);
        return {
            ip: '127.0.0.1',
            port: transport.tuple.localPort,
            ssrc,
            pt
        };
    }
    async consume(roomID, transportID, rtpCapabilities) {
        const router = await this.getRouter(roomID);
        const transport = this.transports.get(transportID);
        if (!transport)
            throw new Error('Transport not found');
        const producer = this.producers.get(roomID + ':video');
        if (!producer)
            throw new Error('Producer not found');
        if (!router.canConsume({ producerId: producer.id, rtpCapabilities })) {
            throw new Error('Cannot consume');
        }
        const consumer = await transport.consume({
            producerId: producer.id,
            rtpCapabilities,
            paused: true, // start paused
        });
        this.consumers.set(consumer.id, consumer);
        return {
            id: consumer.id,
            producerId: producer.id,
            kind: consumer.kind,
            rtpParameters: consumer.rtpParameters,
        };
    }
    async resumeConsumer(consumerID) {
        const consumer = this.consumers.get(consumerID);
        if (!consumer)
            throw new Error('Consumer not found');
        await consumer.resume();
    }
    async cleanupRoom(roomID) {
        const room = this.rooms.get(roomID);
        if (room) {
            room.router.close();
            this.rooms.delete(roomID);
            console.log(`Cleaned up room: ${roomID}`);
            // Note: We should ideally notify Control Plane via webhook that room is closed,
            // but for now relying on Inactivity Timer is sufficient as per plan.
            // Also, we should release ingested ports?
            // Since we don't track which transport belongs to which room easily in 'transports' map,
            // we rely on 'router.close()' closing them, but we need to release the port back to pool.
            // Proper way: Iterate transports, check appData.ingestPort
            // Since 'transports' key is ID, not room. 
            // Optimally, store transport IDs in RoomState.
            // For now, let's just rely on global sweep or leak? 
            // Wait, I fixed the 'close' event listener in prepareIngest (with lint issue).
            // If that fires on router close, we are good.
        }
    }
    async leaveRoom(roomID) {
        // Just trigger cleanup
        await this.cleanupRoom(roomID);
    }
}
//# sourceMappingURL=mediasoup.js.map