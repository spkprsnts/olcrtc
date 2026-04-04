#!/usr/bin/env python3

import asyncio
import json
import uuid
import websockets
import requests
from urllib.parse import quote

CONFERENCE_ID = "46984088311346"
CONFERENCE_URL = f"https://telemost.yandex.ru/j/{CONFERENCE_ID}"
API_BASE = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"

def generate_uuid():
    return str(uuid.uuid4())

def get_connection_info(conference_id, display_name="Guest"):
    url = f"{API_BASE}/conferences/{quote(CONFERENCE_URL, safe='')}/connection"
    params = {
        "next_gen_media_platform_allowed": "true",
        "display_name": display_name,
        "waiting_room_supported": "true"
    }
    
    headers = {
        "User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:149.0) Gecko/20100101 Firefox/149.0",
        "Accept": "*/*",
        "Accept-Language": "en-US,en;q=0.9",
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

async def connect_websocket(ws_url, room_id, peer_id, credentials):
    async with websockets.connect(ws_url) as ws:
        hello_msg = {
            "uid": generate_uuid(),
            "hello": {
                "participantMeta": {
                    "name": "Guest",
                    "role": "SPEAKER",
                    "description": "",
                    "sendAudio": True,
                    "sendVideo": False
                },
                "participantAttributes": {
                    "name": "Guest",
                    "role": "SPEAKER",
                    "description": ""
                },
                "sendAudio": True,
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
                    "simulcastMode": ["DISABLED", "STATIC"],
                    "selfVadStatus": ["FROM_SERVER", "FROM_CLIENT"],
                    "dataChannelSharing": ["TO_RTP"],
                    "videoEncoderConfig": ["NO_CONFIG", "ONLY_INIT_CONFIG", "RUNTIME_CONFIG"],
                    "dataChannelVideoCodec": ["VP8", "UNIQUE_CODEC_FROM_TRACK_DESCRIPTION"],
                    "bandwidthLimitationReason": ["BANDWIDTH_REASON_DISABLED", "BANDWIDTH_REASON_ENABLED"],
                    "sdkDefaultDeviceManagement": ["SDK_DEFAULT_DEVICE_MANAGEMENT_DISABLED", "SDK_DEFAULT_DEVICE_MANAGEMENT_ENABLED"],
                    "joinOrderLayout": ["JOIN_ORDER_LAYOUT_DISABLED", "JOIN_ORDER_LAYOUT_ENABLED"],
                    "pinLayout": ["PIN_LAYOUT_DISABLED"],
                    "sendSelfViewVideoSlot": ["SEND_SELF_VIEW_VIDEO_SLOT_DISABLED", "SEND_SELF_VIEW_VIDEO_SLOT_ENABLED"],
                    "serverLayoutTransition": ["SERVER_LAYOUT_TRANSITION_DISABLED"],
                    "sdkPublisherOptimizeBitrate": ["SDK_PUBLISHER_OPTIMIZE_BITRATE_DISABLED", "SDK_PUBLISHER_OPTIMIZE_BITRATE_FULL", "SDK_PUBLISHER_OPTIMIZE_BITRATE_ONLY_SELF"],
                    "sdkNetworkLostDetection": ["SDK_NETWORK_LOST_DETECTION_DISABLED"],
                    "sdkNetworkPathMonitor": ["SDK_NETWORK_PATH_MONITOR_DISABLED"],
                    "publisherVp9": ["PUBLISH_VP9_DISABLED", "PUBLISH_VP9_ENABLED"],
                    "svcMode": ["SVC_MODE_DISABLED", "SVC_MODE_L3T3", "SVC_MODE_L3T3_KEY"],
                    "subscriberOfferAsyncAck": ["SUBSCRIBER_OFFER_ASYNC_ACK_DISABLED", "SUBSCRIBER_OFFER_ASYNC_ACK_ENABLED"],
                    "androidBluetoothRoutingFix": ["ANDROID_BLUETOOTH_ROUTING_FIX_DISABLED"],
                    "fixedIceCandidatesPoolSize": ["FIXED_ICE_CANDIDATES_POOL_SIZE_DISABLED"],
                    "sdkAndroidTelecomIntegration": ["SDK_ANDROID_TELECOM_INTEGRATION_DISABLED"],
                    "setActiveCodecsMode": ["SET_ACTIVE_CODECS_MODE_DISABLED", "SET_ACTIVE_CODECS_MODE_VIDEO_ONLY"],
                    "subscriberDtlsPassiveMode": ["SUBSCRIBER_DTLS_PASSIVE_MODE_DISABLED"],
                    "publisherOpusDred": ["PUBLISHER_OPUS_DRED_DISABLED"],
                    "publisherOpusLowBitrate": ["PUBLISHER_OPUS_LOW_BITRATE_DISABLED"],
                    "sdkAndroidDestroySessionOnTaskRemoved": ["SDK_ANDROID_DESTROY_SESSION_ON_TASK_REMOVED_DISABLED"],
                    "svcModes": ["FALSE"],
                    "reportTelemetryModes": ["TRUE"],
                    "keepDefaultDevicesModes": ["FALSE"]
                },
                "sdkInfo": {
                    "implementation": "browser",
                    "version": "5.27.0",
                    "userAgent": "Mozilla/5.0 (X11; Linux x86_64; rv:149.0) Gecko/20100101 Firefox/149.0",
                    "hwConcurrency": 24
                },
                "sdkInitializationId": generate_uuid(),
                "disablePublisher": False,
                "disableSubscriber": False,
                "disableSubscriberAudio": False
            }
        }
        
        print(f"[>] Sending hello...")
        await ws.send(json.dumps(hello_msg))
        
        datachannel_found = False
        sctp_port = None
        max_message_size = None
        
        for i in range(20):
            try:
                response = await asyncio.wait_for(ws.recv(), timeout=5.0)
                data = json.loads(response)
                
                msg_types = [k for k in data.keys() if k != 'uid']
                print(f"[<] Message #{i+1}: {msg_types}")
                
                if "ack" in data:
                    continue
                
                if "serverHello" in data:
                    print(f"[+] Got serverHello")
                    ack_msg = {
                        "uid": data["uid"],
                        "ack": {"status": {"code": "OK"}}
                    }
                    await ws.send(json.dumps(ack_msg))
                    print(f"[>] Sent ack for serverHello")
                    continue
                
                if "subscriberSdpOffer" in data:
                    sdp = data["subscriberSdpOffer"].get("sdp", "")
                    print(f"\n[!] Got subscriberSdpOffer! SDP length: {len(sdp)}")
                    
                    if "m=application" in sdp and "SCTP" in sdp:
                        datachannel_found = True
                        
                        for line in sdp.split('\r\n'):
                            if 'sctp-port:' in line:
                                sctp_port = line.split(':')[1].strip()
                            if 'max-message-size:' in line:
                                max_message_size = line.split(':')[1].strip()
                        
                        print(f"[:P] DataChannel FOUND!")
                        print(f"    SCTP port: {sctp_port}")
                        print(f"    Max message size: {max_message_size} bytes")
                        print(f"\n--- DataChannel section in SDP ---")
                        in_app_section = False
                        for line in sdp.split('\r\n'):
                            if 'm=application' in line:
                                in_app_section = True
                            if in_app_section:
                                print(f"  {line}")
                                if line == '':
                                    break
                        print(f"--- End ---\n")
                    else:
                        print(f"[✗] DataChannel NOT found in SDP")
                    
                    ack_msg = {
                        "uid": data["uid"],
                        "ack": {"status": {"code": "OK"}}
                    }
                    await ws.send(json.dumps(ack_msg))
                    print(f"[>] Sent ack for subscriberSdpOffer")
                    break
                    
            except asyncio.TimeoutError:
                print(f"[!] Timeout on message #{i+1}")
                break
            except Exception as e:
                print(f"[!] Error: {e}")
                break
        
        return datachannel_found, sctp_port, max_message_size

async def main():
    print(f"{'='*60}")
    print(f"  Yandex Telemost DataChannel Support Check")
    print(f"{'='*60}\n")
    
    print(f"[1] Getting connection info from API...")
    conn_info = get_connection_info(CONFERENCE_ID)
    
    room_id = conn_info["room_id"]
    peer_id = conn_info["peer_id"]
    credentials = conn_info["credentials"]
    ws_url = conn_info["client_configuration"]["media_server_url"]
    
    print(f"    Room ID: {room_id}")
    print(f"    Peer ID: {peer_id}")
    print(f"    WS URL: {ws_url}\n")
    
    print(f"[2] Connecting to WebSocket and checking SDP...\n")
    dc_supported, sctp_port, max_size = await connect_websocket(ws_url, room_id, peer_id, credentials)
    
    print(f"\n{'='*60}")
    if dc_supported:
        print(f"  :P RESULT: DataChannel IS SUPPORTED")
        print(f"  :P SCTP Port: {sctp_port}")
        print(f"  :P Max Message: {max_size} bytes (~{int(max_size)//1024//1024}MB)")
    else:
        print(f"  ✗ RESULT: DataChannel NOT SUPPORTED")
    print(f"{'='*60}")

if __name__ == "__main__":
    asyncio.run(main())
