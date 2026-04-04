#!/usr/bin/env python3

import asyncio
import json
import uuid
import websockets
import requests
from urllib.parse import quote
from aiortc import RTCPeerConnection, RTCSessionDescription, RTCIceCandidate, RTCConfiguration, RTCIceServer

CONFERENCE_ID = "46984088311346"
CONFERENCE_URL = f"https://telemost.yandex.ru/j/{CONFERENCE_ID}"
API_BASE = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"

def generate_uuid():
    return str(uuid.uuid4())

def get_connection_info(display_name="Server"):
    url = f"{API_BASE}/conferences/{quote(CONFERENCE_URL, safe='')}/connection"
    params = {
        "next_gen_media_platform_allowed": "true",
        "display_name": display_name,
        "waiting_room_supported": "true"
    }
    
    headers = {
        "User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:149.0) Gecko/20100101 Firefox/149.0",
        "Accept": "*/*",
        "content-type": "application/json",
        "Client-Instance-Id": generate_uuid(),
        "X-Telemost-Client-Version": "187.1.0",
        "idempotency-key": generate_uuid(),
        "Origin": "https://telemost.yandex.ru",
        "Referer": "https://telemost.yandex.ru/"
    }
    
    response = requests.get(url, params=params, headers=headers)
    response.raise_for_status()
    return response.json()

async def run_server(ws_url, room_id, peer_id, credentials):
    pc = RTCPeerConnection(RTCConfiguration(
        iceServers=[RTCIceServer(urls=["stun:stun.rtc.yandex.net:3478"])]
    ))
    
    dc_ready = asyncio.Event()
    active_channels = {}
    
    @pc.on("datachannel")
    def on_datachannel(channel):
        print(f"[+] DataChannel received: {channel.label} (id: {channel.id})")
        active_channels[channel.label] = channel
        
        @channel.on("open")
        def on_open():
            print(f"[:P] Channel '{channel.label}' opened")
            dc_ready.set()
        
        @channel.on("message")
        def on_message(message):
            print(f"[<] Received from '{channel.label}': {message[:100]}{'...' if len(message) > 100 else ''}")
            print(f"    Size: {len(message)} bytes")
            
            response = f"Echo: {message}"
            try:
                channel.send(response)
                print(f"[>] Sent echo response")
            except Exception as e:
                print(f"[!] Send error: {e}")
        
        @channel.on("close")
        def on_close():
            print(f"[!] Channel '{channel.label}' closed")
            if channel.label in active_channels:
                del active_channels[channel.label]
    
    async with websockets.connect(ws_url) as ws:
        hello_msg = {
            "uid": generate_uuid(),
            "hello": {
                "participantMeta": {
                    "name": "OlcRTC-Server",
                    "role": "SPEAKER",
                    "description": "",
                    "sendAudio": False,
                    "sendVideo": False
                },
                "participantAttributes": {
                    "name": "OlcRTC-Server",
                    "role": "SPEAKER",
                    "description": ""
                },
                "sendAudio": False,
                "sendVideo": False,
                "sendSharing": False,
                "participantId": peer_id,
                "roomId": room_id,
                "serviceName": "telemost",
                "credentials": credentials,
                "capabilitiesOffer": {
                    "offerAnswerMode": ["SEPARATE"],
                    "initialSubscriberOffer": ["ON_HELLO"],
                    "slotsMode": ["FROM_CONTROLLER"],
                    "simulcastMode": ["DISABLED"],
                    "selfVadStatus": ["FROM_SERVER"],
                    "dataChannelSharing": ["TO_RTP"],
                    "videoEncoderConfig": ["NO_CONFIG"],
                    "dataChannelVideoCodec": ["UNIQUE_CODEC_FROM_TRACK_DESCRIPTION"],
                    "bandwidthLimitationReason": ["BANDWIDTH_REASON_ENABLED"],
                    "sdkDefaultDeviceManagement": ["SDK_DEFAULT_DEVICE_MANAGEMENT_ENABLED"],
                    "joinOrderLayout": ["JOIN_ORDER_LAYOUT_ENABLED"],
                    "pinLayout": ["PIN_LAYOUT_DISABLED"],
                    "sendSelfViewVideoSlot": ["SEND_SELF_VIEW_VIDEO_SLOT_ENABLED"],
                    "serverLayoutTransition": ["SERVER_LAYOUT_TRANSITION_DISABLED"],
                    "sdkPublisherOptimizeBitrate": ["SDK_PUBLISHER_OPTIMIZE_BITRATE_FULL"],
                    "sdkNetworkLostDetection": ["SDK_NETWORK_LOST_DETECTION_DISABLED"],
                    "sdkNetworkPathMonitor": ["SDK_NETWORK_PATH_MONITOR_DISABLED"],
                    "publisherVp9": ["PUBLISH_VP9_ENABLED"],
                    "svcMode": ["SVC_MODE_L3T3_KEY"],
                    "subscriberOfferAsyncAck": ["SUBSCRIBER_OFFER_ASYNC_ACK_DISABLED"],
                    "androidBluetoothRoutingFix": ["ANDROID_BLUETOOTH_ROUTING_FIX_DISABLED"],
                    "fixedIceCandidatesPoolSize": ["FIXED_ICE_CANDIDATES_POOL_SIZE_DISABLED"],
                    "sdkAndroidTelecomIntegration": ["SDK_ANDROID_TELECOM_INTEGRATION_DISABLED"],
                    "setActiveCodecsMode": ["SET_ACTIVE_CODECS_MODE_DISABLED"],
                    "subscriberDtlsPassiveMode": ["SUBSCRIBER_DTLS_PASSIVE_MODE_DISABLED"],
                    "publisherOpusLowBitrate": ["PUBLISHER_OPUS_LOW_BITRATE_DISABLED"],
                    "publisherOpusDred": ["PUBLISHER_OPUS_DRED_DISABLED"],
                    "sdkAndroidDestroySessionOnTaskRemoved": ["SDK_ANDROID_DESTROY_SESSION_ON_TASK_REMOVED_DISABLED"]
                },
                "sdkInfo": {
                    "implementation": "python",
                    "version": "1.0.0",
                    "userAgent": "OlcRTC-Server/1.0",
                    "hwConcurrency": 8
                },
                "sdkInitializationId": generate_uuid(),
                "disablePublisher": False,
                "disableSubscriber": False,
                "disableSubscriberAudio": False
            }
        }
        
        print(f"[>] Sending hello...")
        await ws.send(json.dumps(hello_msg))
        
        async def ws_receiver():
            while True:
                try:
                    response = await ws.recv()
                    data = json.loads(response)
                    
                    msg_types = [k for k in data.keys() if k != 'uid']
                    
                    if "webrtcIceCandidate" not in msg_types and "ack" not in msg_types:
                        print(f"[<] WS: {msg_types}")
                    
                    if "serverHello" in data:
                        ack_msg = {"uid": data["uid"], "ack": {"status": {"code": "OK"}}}
                        await ws.send(json.dumps(ack_msg))
                    
                    if "subscriberSdpOffer" in data:
                        subscriber_sdp = data["subscriberSdpOffer"]["sdp"]
                        print(f"[+] Got subscriber SDP")
                        
                        remote_desc = RTCSessionDescription(sdp=subscriber_sdp, type="offer")
                        await pc.setRemoteDescription(remote_desc)
                        
                        answer = await pc.createAnswer()
                        await pc.setLocalDescription(answer)
                        
                        answer_msg = {
                            "uid": generate_uuid(),
                            "subscriberSdpAnswer": {
                                "pcSeq": data["subscriberSdpOffer"]["pcSeq"],
                                "sdp": pc.localDescription.sdp
                            }
                        }
                        await ws.send(json.dumps(answer_msg))
                        print(f"[>] Sent SDP answer")
                        
                        ack_msg = {"uid": data["uid"], "ack": {"status": {"code": "OK"}}}
                        await ws.send(json.dumps(ack_msg))
                    
                    if "webrtcIceCandidate" in data:
                        cand_data = data["webrtcIceCandidate"]
                        if cand_data.get("target") == "SUBSCRIBER":
                            try:
                                cand_str = cand_data["candidate"]
                                parts = cand_str.split()
                                
                                if len(parts) >= 8:
                                    candidate = RTCIceCandidate(
                                        component=int(parts[1]),
                                        foundation=parts[0].replace("candidate:", ""),
                                        ip=parts[4],
                                        port=int(parts[5]),
                                        priority=int(parts[3]),
                                        protocol=parts[2],
                                        type=parts[7],
                                        sdpMid=cand_data["sdpMid"],
                                        sdpMLineIndex=cand_data["sdpMlineIndex"]
                                    )
                                    await pc.addIceCandidate(candidate)
                            except Exception as e:
                                print(f"[!] ICE error: {e}")
                    
                except Exception as e:
                    print(f"[!] WS error: {e}")
                    break
        
        @pc.on("icecandidate")
        async def on_icecandidate(event):
            if event.candidate:
                candidate = event.candidate
                ice_msg = {
                    "uid": generate_uuid(),
                    "webrtcIceCandidate": {
                        "candidate": candidate.candidate,
                        "sdpMid": candidate.sdpMid,
                        "sdpMlineIndex": candidate.sdpMLineIndex,
                        "usernameFragment": "",
                        "target": "SUBSCRIBER",
                        "pcSeq": 1
                    }
                }
                await ws.send(json.dumps(ice_msg))
        
        ws_task = asyncio.create_task(ws_receiver())
        
        print(f"[*] Server listening for DataChannel connections...")
        print(f"[*] Press Ctrl+C to stop\n")
        
        try:
            while True:
                await asyncio.sleep(1)
        except KeyboardInterrupt:
            print(f"\n[*] Shutting down...")
        finally:
            ws_task.cancel()
            await pc.close()

async def main():
    print(f"{'='*60}")
    print(f"  OlcRTC Server - Yandex Telemost DataChannel")
    print(f"{'='*60}\n")
    
    print(f"[1] Connecting to conference...")
    conn_info = get_connection_info()
    
    room_id = conn_info["room_id"]
    peer_id = conn_info["peer_id"]
    credentials = conn_info["credentials"]
    ws_url = conn_info["client_configuration"]["media_server_url"]
    
    print(f"    Room: {room_id}")
    print(f"    Peer: {peer_id}\n")
    
    print(f"[2] Starting server...\n")
    await run_server(ws_url, room_id, peer_id, credentials)

if __name__ == "__main__":
    asyncio.run(main())
