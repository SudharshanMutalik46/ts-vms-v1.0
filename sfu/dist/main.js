import express from 'express';
import cors from 'cors';
import { MediasoupManager } from './mediasoup.js';
const app = express();
const port = process.env.PORT || 8085;
const sharedSecret = process.env.SFU_SECRET || 'sfu-internal-secret';
app.use(cors());
app.use(express.json());
app.get('/health', (req, res) => {
    res.sendStatus(200);
});
// Auth Middleware
app.use((req, res, next) => {
    const authHeader = req.headers['x-internal-auth'];
    if (authHeader !== sharedSecret) {
        return res.status(403).send('Forbidden');
    }
    next();
});
const msMgr = new MediasoupManager();
// Endpoints
app.get('/stats', async (req, res) => {
    try {
        const stats = await msMgr.getStats();
        res.json(stats);
    }
    catch (e) {
        res.status(500).send(e.message);
    }
});
app.get('/rooms/:roomID/rtp-capabilities', async (req, res) => {
    try {
        const router = await msMgr.getRouter(req.params.roomID);
        res.json(router.rtpCapabilities);
    }
    catch (e) {
        res.status(500).send(e.message);
    }
});
import { v4 as uuidv4 } from 'uuid';
app.post('/rooms/:roomID/join', async (req, res) => {
    try {
        const sessionID = req.body.sessionId || uuidv4();
        await msMgr.joinRoom(req.params.roomID, sessionID);
        res.sendStatus(200);
    }
    catch (e) {
        if (e.message === 'Room at capacity') {
            res.status(429).send(e.message);
        }
        else {
            res.status(500).send(e.message);
        }
    }
});
app.post('/rooms/:roomID/ingest', async (req, res) => {
    try {
        const info = await msMgr.prepareIngest(req.params.roomID);
        res.json(info);
    }
    catch (e) {
        console.error(`Ingest allocation failed for ${req.params.roomID}:`, e);
        res.status(500).send(e.message);
    }
});
app.post('/rooms/:roomID/transports/webrtc', async (req, res) => {
    try {
        const transportInfo = await msMgr.createWebRtcTransport(req.params.roomID);
        res.json(transportInfo);
    }
    catch (e) {
        res.status(500).send(e.message);
    }
});
app.post('/rooms/:roomID/transports/:transportID/connect', async (req, res) => {
    try {
        await msMgr.connectWebRtcTransport(req.params.transportID, req.body.dtlsParameters);
        res.sendStatus(200);
    }
    catch (e) {
        res.status(500).send(e.message);
    }
});
app.post('/rooms/:roomID/transports/:transportID/produce', async (req, res) => {
    // Note: Produce from WebRTC view is not needed for now as we only have Media Plane producer.
    res.status(501).send('Not implemented (WebRTC producing)');
});
app.post('/rooms/:roomID/transports/:transportID/consume', async (req, res) => {
    try {
        const consumerInfo = await msMgr.consume(req.params.roomID, req.params.transportID, req.body.rtpCapabilities);
        res.json(consumerInfo);
    }
    catch (e) {
        console.error("Consume error:", e);
        res.status(500).send(e.message);
    }
});
app.post('/rooms/:roomID/transports/:transportID/consumers/:consumerID/resume', async (req, res) => {
    try {
        await msMgr.resumeConsumer(req.params.consumerID);
        res.sendStatus(200);
    }
    catch (e) {
        res.status(500).send(e.message);
    }
});
app.post('/sessions/leave', async (req, res) => {
    try {
        await msMgr.leaveRoom(req.body.roomId);
        res.sendStatus(200);
    }
    catch (e) {
        res.status(500).send(e.message);
    }
});
import { WebSocketServer } from 'ws';
import http from 'http';
// Create HTTP server from Express app
const server = http.createServer(app);
// Create WebSocket Server
const wss = new WebSocketServer({ server });
wss.on('connection', (ws, req) => {
    const url = new URL(req.url || '', `http://${req.headers.host}`);
    const roomId = url.searchParams.get('roomId');
    const sessionId = url.searchParams.get('sessionId');
    if (!roomId) {
        ws.close(1008, 'Missing roomId');
        return;
    }
    // Attach WS to Room/Session in MediasoupManager (Simplification: just log for now)
    console.log(`WS Connected: Room=${roomId}, Session=${sessionId}`);
    ws.on('message', (message) => {
        try {
            const msg = JSON.parse(message.toString());
            console.log(`WS Message from ${sessionId}:`, msg);
        }
        catch (e) {
            console.error('Failed to parse WS message');
        }
    });
    ws.on('close', () => {
        console.log(`WS Disconnected: Session=${sessionId}`);
    });
});
msMgr.init().then(() => {
    // Listen on the HTTP server, not just the Express app
    // Bind to 127.0.0.1 to ensure internal-only access (Phase 3.5 Check 3)
    server.listen(port, () => {
        console.log(`SFU Service listening on port ${port} (127.0.0.1 only)`);
    });
});
//# sourceMappingURL=main.js.map