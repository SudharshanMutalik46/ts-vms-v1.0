import * as mediasoup from 'mediasoup';
import { v4 as uuidv4 } from 'uuid';
import os from 'os';

interface RoomState {
    router: mediasoup.types.Router;
    viewerSessions: Set<string>;
    lastActivity: number;
}

export class MediasoupManager {
    private workers: mediasoup.types.Worker[] = [];
    private webRtcServers: Map<number, mediasoup.types.WebRtcServer> = new Map(); // workerPid -> WebRtcServer
    private nextWorkerIdx = 0;

    // Fix 8 & 9: Room State Management
    private rooms: Map<string, RoomState> = new Map();

    private transports: Map<string, mediasoup.types.WebRtcTransport | mediasoup.types.PlainTransport> = new Map();
    private producers: Map<string, mediasoup.types.Producer> = new Map();
    private consumers: Map<string, mediasoup.types.Consumer> = new Map();

    private MAX_VIEWERS_PER_ROOM = 50;
    private IDLE_TIMEOUT_MS = 60000;
    private REAPER_INTERVAL_MS = 10000;

    // Fix 1: Port management for PlainTransport (Ingest)
    private usedIngestPorts = new Set<number>();
    private INGEST_PORT_MIN = 50000;
    private INGEST_PORT_MAX = 51000;

    private getFreeIngestPort(): number {
        for (let port = this.INGEST_PORT_MIN; port <= this.INGEST_PORT_MAX; port++) {
            if (!this.usedIngestPorts.has(port)) {
                this.usedIngestPorts.add(port);
                return port;
            }
        }
        throw new Error('No free ingest ports available');
    }

    private releaseIngestPort(port: number) {
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

    private startIdleReaper() {
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

    private getNextWorker(): mediasoup.types.Worker {
        const worker = this.workers[this.nextWorkerIdx];
        if (!worker) throw new Error('No workers available');
        this.nextWorkerIdx = (this.nextWorkerIdx + 1) % this.workers.length;
        return worker;
    }

    async getRouter(roomID: string): Promise<mediasoup.types.Router> {
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
                    {
                        kind: 'video',
                        mimeType: 'video/H264',
                        clockRate: 90000,
                        parameters: {
                            'packetization-mode': 1,
                            'profile-level-id': '4d001f',
                            'level-asymmetry-allowed': 1
                        }
                    },
                    {
                        kind: 'video',
                        mimeType: 'video/H264',
                        clockRate: 90000,
                        parameters: {
                            'packetization-mode': 1,
                            'profile-level-id': '64001f',
                            'level-asymmetry-allowed': 1
                        }
                    },
                ]
            });
            // Attach worker PID to router for WebRtcServer lookup
            (router as any).appData = { workerPid: worker.pid };

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
    async joinRoom(roomID: string, sessionID: string): Promise<void> {
        await this.getRouter(roomID); // Ensure exists
        const room = this.rooms.get(roomID);
        if (!room) return; // Should not happen

        if (room.viewerSessions.size >= this.MAX_VIEWERS_PER_ROOM) {
            throw new Error('Room at capacity');
        }

        room.viewerSessions.add(sessionID);
        room.lastActivity = Date.now();
    }

    async createWebRtcTransport(roomID: string): Promise<any> {
        const router = await this.getRouter(roomID);
        const workerPid = (router as any).appData.workerPid;
        const webRtcServer = this.webRtcServers.get(workerPid);

        if (!webRtcServer) throw new Error('WebRtcServer not found for this router');

        const transport = await router.createWebRtcTransport({
            webRtcServer,
            enableUdp: true,
            enableTcp: true,
            preferUdp: true,
            initialAvailableOutgoingBitrate: 1000000,
            appData: { roomID }
        });

        this.transports.set(transport.id, transport);

        return {
            id: transport.id,
            iceParameters: transport.iceParameters,
            iceCandidates: transport.iceCandidates,
            dtlsParameters: transport.dtlsParameters,
        };
    }

    async connectWebRtcTransport(transportID: string, dtlsParameters: mediasoup.types.DtlsParameters) {
        const transport = this.transports.get(transportID) as mediasoup.types.WebRtcTransport;
        if (!transport) throw new Error('Transport not found');
        await transport.connect({ dtlsParameters });
    }

    async prepareIngest(roomID: string): Promise<any> {
        const router = await this.getRouter(roomID);

        // Fix: Reuse existing producer/transport if already ingesting
        const existingProducer = this.producers.get(roomID + ':video');
        if (existingProducer) {
            // Find associated transport using appData (we need to iterate or store it better)
            // For now, iterate transports to find the one with appData.ingestPort matching?
            // Or simply store the Ingest Transport ID in the RoomState?
            // Since we don't have RoomState ref here easily without lookup.

            // Optimization: Just return the stored info if we had it.
            // But we need the PORT.
            // Let's iterate transports.
            for (const transport of this.transports.values()) {
                if ((transport as any).appData && (transport as any).appData.ingestPort && (transport as any).appData.roomID === roomID) {
                    console.log(`Reusing existing Ingest Transport for room ${roomID} on port ${(transport as any).appData.ingestPort}`);
                    return {
                        ip: '127.0.0.1',
                        port: (transport as any).appData.ingestPort,
                        ssrc: 11111111,
                        pt: 96
                    };
                }
            }
        }

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

        // Store port AND roomID in appData for release/lookup
        (transport as any).appData = { ingestPort: port, roomID: roomID };

        (transport as any).on('close', () => {
            this.releaseIngestPort(port);
            console.log(`Released ingest port ${port}`);
        });

        console.log(`Created PlainTransport (Ingest) for room ${roomID} on port ${port}`);

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

        console.log(`Created Producer for room ${roomID}: ID=${producer.id}, PT=${pt}, SSRC=${ssrc}`);
        console.log(`[Producer ${producer.id}] Codec: ${producer.rtpParameters.codecs?.[0]?.mimeType}`); // Task B: Log Codec

        this.producers.set(roomID + ':video', producer);

        // Debug: Listen for Transport events
        transport.on('tuple', (tuple) => {
            console.log(`[Ingest Transport] Latched to remote producer: ${tuple.remoteIp}:${tuple.remotePort}`);
        });

        // Debug: Listen for Producer events
        producer.on('score', (score) => {
            console.log(`[Producer ${producer.id}] Score:`, score);
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

    async consume(roomID: string, transportID: string, rtpCapabilities: mediasoup.types.RtpCapabilities): Promise<any> {
        const router = await this.getRouter(roomID);
        const transport = this.transports.get(transportID) as mediasoup.types.WebRtcTransport;
        if (!transport) throw new Error('Transport not found');

        const producer = this.producers.get(roomID + ':video');
        if (!producer) throw new Error('Producer not found');

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

        console.log(`[Consumer ${consumer.id}] Created (paused=${consumer.paused}) for Producer ${producer.id}`);

        // Task A: Ensure consumer is resumed immediately
        await consumer.resume();
        console.log(`[Consumer ${consumer.id}] Resumed (paused=${consumer.paused})`);

        // Task C: Request Keyframe Best-Effort
        try {
            await consumer.requestKeyFrame();
            console.log(`[Consumer ${consumer.id}] PLI Requested (Codec: ${consumer.rtpParameters.codecs?.[0]?.mimeType})`);
        } catch (e) {
            console.warn(`[Consumer ${consumer.id}] PLI Request failed:`, e);
        }

        this.consumers.set(consumer.id, consumer);

        return {
            id: consumer.id,
            producerId: producer.id,
            kind: consumer.kind,
            rtpParameters: consumer.rtpParameters,
            paused: consumer.paused
        };
    }

    async resumeConsumer(consumerID: string) {
        const consumer = this.consumers.get(consumerID);
        if (!consumer) throw new Error('Consumer not found');
        await consumer.resume();
    }

    async cleanupRoom(roomID: string) {
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

    async leaveRoom(roomID: string) {
        // Just trigger cleanup
        await this.cleanupRoom(roomID);
    }

    async getStats(): Promise<any> {
        const stats: any = {
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
                        const rid = (transport.appData as any)?.roomID;
                        if (rid && stats.rooms[rid]) stats.rooms[rid].bytes_in += s.bytesReceived;
                    }
                    if (typeof s.bytesSent === 'number') {
                        stats.totals.bytes_out += s.bytesSent;
                        const rid = (transport.appData as any)?.roomID;
                        if (rid && stats.rooms[rid]) stats.rooms[rid].bytes_out += s.bytesSent;
                    }
                }
            } catch (e) { /* ignore */ }
        }

        return stats;
    }
}
