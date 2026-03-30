import * as mediasoup from 'mediasoup';
import { v4 as uuidv4 } from 'uuid';
import os from 'os';
export class MediasoupManager {
    workers = [];
    webRtcServers = new Map(); // workerPid -> WebRtcServer
    nextWorkerIdx = 0;
    h265Supported = false;
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
    // PlainTransport with rtcpMux=false needs a second UDP port for RTCP.
    // Reserve RTP/RTCP as an even/odd pair to avoid self-collisions.
    INGEST_PORT_MIN = 52000;
    INGEST_PORT_MAX = 58999;
    reserveIngestPortPair() {
        const start = this.INGEST_PORT_MIN % 2 === 0 ? this.INGEST_PORT_MIN : this.INGEST_PORT_MIN + 1;
        for (let rtpPort = start; rtpPort + 1 <= this.INGEST_PORT_MAX; rtpPort += 2) {
            const rtcpPort = rtpPort + 1;
            if (!this.usedIngestPorts.has(rtpPort) && !this.usedIngestPorts.has(rtcpPort)) {
                this.usedIngestPorts.add(rtpPort);
                this.usedIngestPorts.add(rtcpPort);
                return { rtpPort, rtcpPort };
            }
        }
        throw new Error('No free ingest ports available');
    }
    releaseIngestPortPair(rtpPort, rtcpPort) {
        this.usedIngestPorts.delete(rtpPort);
        this.usedIngestPorts.delete(rtcpPort);
    }
    async init() {
        const numWorkers = os.cpus().length;
        const localIp = process.env['ANNOUNCED_IP'] || '127.0.0.1'; // Ensure this is set correctly in prod
        for (let i = 0; i < numWorkers; i++) {
            // Fix 3: Use WebRtcServer for better UDP/TCP control
            const worker = await mediasoup.createWorker({
                logLevel: 'warn',
                rtcMinPort: 40000,
                rtcMaxPort: 49999,
            });
            // Create WebRtcServer per worker
            // AFTER — UDP only, avoids DTLS-over-TCP issues with WebView2:
            const webRtcServer = await worker.createWebRtcServer({
                listenInfos: [
                    {
                        protocol: 'udp',
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
        // Detect H265 support
        const firstWorker = this.workers[0];
        if (firstWorker) {
            try {
                const tempRouter = await firstWorker.createRouter({
                    mediaCodecs: [{ kind: 'video', mimeType: 'video/H265', clockRate: 90000, parameters: {} }]
                });
                this.h265Supported = true;
                tempRouter.close();
            }
            catch (e) {
                console.log('H265 not supported by this mediasoup build/environment; using H264 fallback');
                this.h265Supported = false;
            }
        }
        console.log(`Initialized ${this.workers.length} mediasoup workers (H265=${this.h265Supported})`);
        // Fix 9: Start Idle Reaper
        this.startIdleReaper();
    }
    supportsH265() {
        return this.h265Supported;
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
        if (room)
            return room.router;
        const worker = this.getNextWorker();
        const h264Codecs = [
            {
                kind: 'video',
                mimeType: 'video/H264',
                clockRate: 90000,
                parameters: {
                    'packetization-mode': 1,
                    'profile-level-id': '42001f',
                    'level-asymmetry-allowed': 1,
                    'x-google-start-bitrate': 1000
                }
            }
        ];
        const router = await worker.createRouter({ mediaCodecs: h264Codecs });
        console.log(`Created router for room: ${roomID} (H264 constrained-baseline)`);
        // Attach worker PID to router for WebRtcServer lookup
        router.appData = { workerPid: worker.pid };
        room = {
            router,
            viewerSessions: new Set(),
            lastActivity: Date.now()
        };
        this.rooms.set(roomID, room);
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
            enableTcp: false, // disable TCP entirely
            preferUdp: true,
            initialAvailableOutgoingBitrate: 1000000,
            appData: { roomID }
        });
        this.transports.set(transport.id, transport);
        // Diagnostic: log DTLS and ICE state transitions
        transport.on('dtlsstatechange', (dtlsState) => {
            console.log(`[Transport ${transport.id}] DTLS: ${dtlsState}`);
        });
        transport.on('icestatechange', (iceState) => {
            console.log(`[Transport ${transport.id}] ICE: ${iceState}`);
        });
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
    async prepareIngest(roomID, codec = 'H264') {
        const router = await this.getRouter(roomID);
        if (codec === 'H265') {
            console.log(`prepareIngest requested H265 for ${roomID}; forcing H264 constrained-baseline ingest`);
            codec = 'H264';
        }
        // Reuse existing producer/transport if already ingesting (and not closed)
        const existingProducer = this.producers.get(roomID + ':video');
        const existingCodec = normalizeCodecName(existingProducer?.rtpParameters?.codecs?.[0]?.mimeType);
        if (existingProducer && !existingProducer.closed && existingCodec === codec) {
            for (const transport of this.transports.values()) {
                if (transport.appData && transport.appData.ingestPort && transport.appData.roomID === roomID && !transport.closed) {
                    console.log(`Reusing existing Ingest Transport for room ${roomID} on port ${transport.appData.ingestPort}`);
                    return {
                        ip: '127.0.0.1',
                        port: transport.appData.ingestPort,
                        ssrc: 11111111,
                        pt: 96
                    };
                }
            }
        }
        // If we have a stale ingest (producer exists but transport not reusable), close it to free ports.
        if (existingProducer && !existingProducer.closed && existingCodec === codec) {
            try {
                existingProducer.close();
            }
            catch { }
            this.producers.delete(roomID + ':video');
            for (const [tid, transport] of this.transports) {
                if (transport.appData?.roomID === roomID && transport.appData?.ingestPort !== undefined) {
                    try {
                        transport.close();
                    }
                    catch { }
                    this.transports.delete(tid);
                }
            }
        }
        // Clean up stale closed producer entry if present
        if (existingProducer && existingProducer.closed) {
            this.producers.delete(roomID + ':video');
        }
        const localIp = process.env['ANNOUNCED_IP'] || '127.0.0.1';
        // Retry port allocation: skip ports already in use by other processes.
        let transport;
        let rtpPort = 0;
        let rtcpPort = 0;
        const MAX_PORT_RETRIES = Math.max(50, Math.floor((this.INGEST_PORT_MAX - this.INGEST_PORT_MIN) / 2));
        for (let attempt = 0; attempt < MAX_PORT_RETRIES; attempt++) {
            ({ rtpPort, rtcpPort } = this.reserveIngestPortPair());
            try {
                transport = await router.createPlainTransport({
                    listenInfo: {
                        protocol: 'udp',
                        ip: '0.0.0.0',
                        announcedAddress: localIp,
                        port: rtpPort
                    },
                    rtcpListenInfo: {
                        protocol: 'udp',
                        ip: '0.0.0.0',
                        announcedAddress: localIp,
                        port: rtcpPort
                    },
                    rtcpMux: false,
                    comedia: true,
                });
                break; // success
            }
            catch (e) {
                if (e.message && e.message.includes('address already in use')) {
                    console.log(`Port ${rtpPort}/${rtcpPort} already in use (attempt ${attempt + 1}), trying next`);
                    // Important: release the reserved pair; bind never succeeded.
                    this.releaseIngestPortPair(rtpPort, rtcpPort);
                    continue;
                }
                this.releaseIngestPortPair(rtpPort, rtcpPort);
                throw e; // unexpected error
            }
        }
        if (!transport)
            throw new Error('No free ingest ports available after retries');
        // Store port AND roomID in appData for release/lookup
        transport.appData = { ingestPort: rtpPort, ingestRtcpPort: rtcpPort, roomID: roomID };
        transport.on('close', () => {
            this.releaseIngestPortPair(rtpPort, rtcpPort);
            console.log(`Released ingest ports ${rtpPort}/${rtcpPort}`);
        });
        console.log(`Created PlainTransport (Ingest) for room ${roomID} on ports ${rtpPort}/${rtcpPort}`);
        this.transports.set(transport.id, transport);
        // For simplicity, we use hardcoded SSRC/PT for now or generate them
        const ssrc = 11111111;
        const pt = 96;
        const codecDef = {
            mimeType: 'video/H264',
            payloadType: pt,
            clockRate: 90000,
            parameters: {
                'packetization-mode': 1,
                'profile-level-id': '42001f',
                'level-asymmetry-allowed': 1,
                'x-google-start-bitrate': 1000
            }
        };
        const producer = await transport.produce({
            kind: 'video',
            rtpParameters: {
                codecs: [codecDef],
                encodings: [{ ssrc }]
            }
        });
        console.log(`Created Producer for room ${roomID}: ID=${producer.id}, PT=${pt}, SSRC=${ssrc}`);
        console.log(`[Producer ${producer.id}] Codec: ${producer.rtpParameters.codecs?.[0]?.mimeType}`); // Task B: Log Codec
        this.producers.set(roomID + ':video', producer);
        // Debug: Listen for Transport events
        transport.on('tuple', (tuple) => {
            console.log(`[Ingest Transport] Latched to remote producer: ${tuple.remoteIp}:${tuple.remotePort}`);
        });
        // Debug: Listen for Producer events
        producer.on('score', (score) => {
            console.log(`[Producer ${producer.id}] Score:`, JSON.stringify(score));
        });
        producer.on('videoorientationchange', (videoOrientation) => {
            console.log(`[Producer ${producer.id}] Video Orientation:`, videoOrientation);
        });
        producer.on('trace', (trace) => {
            console.log(`[Producer ${producer.id}] Trace:`, trace);
        });
        console.log('Ingest Info ready:', {
            ip: '127.0.0.1',
            port: transport.tuple.localPort,
            ssrc,
            pt
        });
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
            console.error(`[Consume Error] Router cannot consume (Producer: ${producer.id}, Room: ${roomID})`);
            console.error(`[Consume Error] Producer RTP Parameters:`, JSON.stringify(producer.rtpParameters, null, 2));
            console.error(`[Consume Error] Client RTP Capabilities:`, JSON.stringify(rtpCapabilities, null, 2));
            throw new Error('Cannot consume');
        }
        const consumer = await transport.consume({
            producerId: producer.id,
            rtpCapabilities,
            paused: true, // start paused, then resume
        });
        const cSSRC = consumer.rtpParameters?.encodings?.[0]?.ssrc;
        const cPT = consumer.rtpParameters?.codecs?.[0]?.payloadType;
        console.log(`[Consumer ${consumer.id}] Created (paused=${consumer.paused}) PT=${cPT} SSRC=${cSSRC} for Producer ${producer.id}`);
        this.consumers.set(consumer.id, consumer);
        return {
            id: consumer.id,
            producerId: producer.id,
            kind: consumer.kind,
            rtpParameters: consumer.rtpParameters,
            paused: consumer.paused
        };
    }
    async resumeConsumer(consumerID) {
        const consumer = this.consumers.get(consumerID);
        if (!consumer)
            throw new Error('Consumer not found');
        await consumer.resume();
        console.log(`[Consumer ${consumer.id}] Resumed`);
        // Request keyframe after resume — spread over the full 20s viewer timeout.
        // The H265→H264 bridge pipeline (nvh265dec/mfh264enc) needs up to 5s to
        // produce its first IDR; the old 6×400ms window ended before the bridge
        // was ready, so no keyframe was ever delivered.
        for (let i = 0; i < 10; i++) {
            setTimeout(async () => {
                try {
                    await consumer.requestKeyFrame();
                    console.log(`[Consumer ${consumer.id}] PLI Requested (retry=${i + 1}, codec=${consumer.rtpParameters.codecs?.[0]?.mimeType})`);
                }
                catch (e) {
                    console.warn(`[Consumer ${consumer.id}] PLI Request failed on retry ${i + 1}:`, e);
                }
            }, i * 2000);
        }
    }
    async cleanupRoom(roomID) {
        const room = this.rooms.get(roomID);
        if (room) {
            // Close consumers belonging to this room.
            for (const [cid, consumer] of this.consumers) {
                const t = this.transports.get(consumer.transportId);
                if (t && t.appData?.roomID === roomID) {
                    try {
                        consumer.close();
                    }
                    catch { }
                    this.consumers.delete(cid);
                }
            }
            // Close producer for this room (ingest).
            const producer = this.producers.get(roomID + ':video');
            if (producer) {
                try {
                    producer.close();
                }
                catch { }
                this.producers.delete(roomID + ':video');
            }
            // Close transports belonging to this room (WebRTC + ingest PlainTransport).
            for (const [tid, transport] of this.transports) {
                const appData = transport.appData;
                if (appData?.roomID === roomID) {
                    try {
                        transport.close();
                    }
                    catch { }
                    this.transports.delete(tid);
                }
            }
            try {
                room.router.close();
            }
            catch { }
            this.rooms.delete(roomID);
            console.log(`Cleaned up room: ${roomID}`);
        }
    }
    async leaveRoom(roomID) {
        // Just trigger cleanup
        await this.cleanupRoom(roomID);
    }
    async getStats() {
        const stats = {
            totals: {
                rooms: this.rooms.size,
                workers: this.workers.length,
                producers: this.producers.size,
                consumers: this.consumers.size,
                transports: this.transports.size,
                bytes_in: 0,
                bytes_out: 0
            },
            rooms: {}
        };
        // Populate room basic stats
        for (const [id, room] of this.rooms) {
            stats.rooms[id] = {
                viewers: room.viewerSessions.size,
                producers: this.producers.has(id + ':video') ? 1 : 0,
                bytes_in: 0,
                bytes_out: 0
            };
        }
        // Aggregate bytes
        for (const transport of this.transports.values()) {
            try {
                const tStats = await transport.getStats();
                for (const s of tStats) {
                    // Check generic stats structure (WebRtcTransportStats or PlainTransportStats)
                    if (typeof s.bytesReceived === 'number') {
                        stats.totals.bytes_in += s.bytesReceived;
                        const rid = transport.appData?.roomID;
                        if (rid && stats.rooms[rid])
                            stats.rooms[rid].bytes_in += s.bytesReceived;
                    }
                    if (typeof s.bytesSent === 'number') {
                        stats.totals.bytes_out += s.bytesSent;
                        const rid = transport.appData?.roomID;
                        if (rid && stats.rooms[rid])
                            stats.rooms[rid].bytes_out += s.bytesSent;
                    }
                }
            }
            catch (e) { /* ignore */ }
        }
        return stats;
    }
}
function normalizeCodecName(mimeType) {
    const value = String(mimeType || '').toUpperCase();
    if (value.includes('H265') || value.includes('HEVC'))
        return 'H265';
    if (value.includes('H264'))
        return 'H264';
    return '';
}
//# sourceMappingURL=mediasoup.js.map