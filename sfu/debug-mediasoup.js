import * as mediasoup from 'mediasoup';

async function checkCapabilities() {
    console.log("Creating worker...");
    const worker = await mediasoup.createWorker();

    console.log("Creating router to check defaults...");

    const h265Codec = {
        kind: 'video',
        mimeType: 'video/H265',
        clockRate: 90000,
        parameters: {
            'packetization-mode': 1,
            'profile-id': 1,
            'tier-flag': 0,
            'level-id': 120
        }
    };

    console.log("Attempting to create Router with H.265...");
    try {
        const routerH265 = await worker.createRouter({
            mediaCodecs: [h265Codec]
        });
        console.log("SUCCESS: Router created with H.265 support!");
        console.log("Router RTP Capabilities:", JSON.stringify(routerH265.rtpCapabilities, null, 2));
        routerH265.close();
    } catch (e) {
        console.error("FAILURE: Could not create Router with H.265:", e.message);

        // Try relaxed parameters
        try {
            const routerRelaxed = await worker.createRouter({
                mediaCodecs: [{
                    kind: 'video',
                    mimeType: 'video/H265',
                    clockRate: 90000,
                    parameters: {}
                }]
            });
            console.log("SUCCESS: Router created with H.265 (Relaxed permissions)!");
            console.log("Router RTP Capabilities (Relaxed):", JSON.stringify(routerRelaxed.rtpCapabilities, null, 2));
            routerRelaxed.close();
        } catch (e2) {
            console.error("FAILURE: Could not create Router with H.265 (Relaxed):", e2.message);
        }
    }

    worker.close();
}

checkCapabilities();
