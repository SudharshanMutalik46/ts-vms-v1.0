import * as mediasoup from 'mediasoup';
export declare class MediasoupManager {
    private workers;
    private webRtcServers;
    private nextWorkerIdx;
    private rooms;
    private transports;
    private producers;
    private consumers;
    private MAX_VIEWERS_PER_ROOM;
    private IDLE_TIMEOUT_MS;
    private REAPER_INTERVAL_MS;
    private usedIngestPorts;
    private INGEST_PORT_MIN;
    private INGEST_PORT_MAX;
    private getFreeIngestPort;
    private releaseIngestPort;
    init(): Promise<void>;
    private startIdleReaper;
    private getNextWorker;
    getRouter(roomID: string): Promise<mediasoup.types.Router>;
    joinRoom(roomID: string, sessionID: string): Promise<void>;
    createWebRtcTransport(roomID: string): Promise<any>;
    connectWebRtcTransport(transportID: string, dtlsParameters: mediasoup.types.DtlsParameters): Promise<void>;
    prepareIngest(roomID: string): Promise<any>;
    consume(roomID: string, transportID: string, rtpCapabilities: mediasoup.types.RtpCapabilities): Promise<any>;
    resumeConsumer(consumerID: string): Promise<void>;
    cleanupRoom(roomID: string): Promise<void>;
    leaveRoom(roomID: string): Promise<void>;
}
//# sourceMappingURL=mediasoup.d.ts.map