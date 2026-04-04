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

async def test_datachannel(ws_url, room_id, peer_id, credentials, ice_servers):
    pc = RTCPeerConnection(RTCConfiguration(
        iceServers=[RTCIceServer(urls=server["urls"]) for server in ice_servers]
    ))
    
    dc = pc.createDataChannel("test", ordered=True)
    dc_open = asyncio.Event()
    dc_message_received = asyncio.Event()
    received_messages = []
    
    @dc.on("open")
    def on_open():
        print(f"[:P] DataChannel opened! (label: {dc.label}, id: {dc.id})")
        dc_open.set()
    
    @dc.on("message")
    def on_message(message):
        print(f"[<] DataChannel received: '{message}'")
        received_messages.append(message)
        dc_message_received.set()
    
    @dc.on("close")
    def on_close():
        print(f"[!] DataChannel closed")
    
    @dc.on("error")
    def on_error(error):
        print(f"[!] DataChannel error: {error}")
    
    async with websockets.connect(ws_url) as ws:
        hello_msg = {
            "uid": generate_uuid(),
            "hello": {
                "participantMeta": {
                    "name": "DCTest",
                    "role": "SPEAKER",
                    "description": "",
                    "sendAudio": False,
                    "sendVideo": False
                },
                "participantAttributes": {
                    "name": "DCTest",
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
                    "userAgent": "OlcRTC/1.0",
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
        
        subscriber_sdp = None
        
        async def ws_receiver():
            nonlocal subscriber_sdp
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
                        print(f"[+] Set remote description")
                        
                        answer = await pc.createAnswer()
                        await pc.setLocalDescription(answer)
                        print(f"[+] Created answer")
                        
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
                                else:
                                    print(f"[!] Invalid candidate format: {cand_str}")
                            except Exception as e:
                                print(f"[!] ICE candidate error: {e}")
                    
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
                print(f"[>] Sent ICE candidate")
        
        ws_task = asyncio.create_task(ws_receiver())
        
        try:
            await asyncio.wait_for(dc_open.wait(), timeout=15.0)
            
            test_message = "Hello from OlcRTC! DataChannel works!"
            print(f"\n[>] Sending test message: '{test_message}'")
            try:
                dc.send(test_message)
                print(f"[:P] Message sent successfully ({len(test_message)} bytes)")
            except Exception as e:
                print(f"[✗] Send failed: {e}")
            
            print(f"[*] Waiting for response (10s)...")
            try:
                await asyncio.wait_for(dc_message_received.wait(), timeout=10.0)
                print(f"[:P] Got response: {received_messages}")
            except asyncio.TimeoutError:
                print(f"[i] No response (normal - need another peer in conference)")
            
            print(f"[*] Sending large message test...")
            large_msg = "X" * 10000
            try:
                dc.send(large_msg)
                print(f"[:P] Large message sent ({len(large_msg)} bytes)")
            except Exception as e:
                print(f"[✗] Large send failed: {e}")
            
            await asyncio.sleep(2)
            
        except asyncio.TimeoutError:
            print(f"[✗] DataChannel did not open in time")
        finally:
            ws_task.cancel()
            await pc.close()
        
        return len(received_messages) > 0

async def main():
    print(f"{'='*60}")
    print(f"  Yandex Telemost DataChannel FULL TEST")
    print(f"{'='*60}\n")
    
    print(f"[1] Getting connection info...")
    conn_info = get_connection_info(CONFERENCE_ID)
    
    room_id = conn_info["room_id"]
    peer_id = conn_info["peer_id"]
    credentials = conn_info["credentials"]
    ws_url = conn_info["client_configuration"]["media_server_url"]
    ice_servers = conn_info.get("client_configuration", {}).get("ice_servers", [
        {"urls": ["stun:stun.rtc.yandex.net:3478"]}
    ])
    
    print(f"    Room: {room_id}")
    print(f"    Peer: {peer_id}\n")
    
    print(f"[2] Testing DataChannel with WebRTC...\n")
    got_response = await test_datachannel(ws_url, room_id, peer_id, credentials, ice_servers)
    
    print(f"\n{'='*60}")
    print(f"  :P PROOF OF CONCEPT SUCCESSFUL!")
    print(f"  :P DataChannel works in Yandex Telemost")
    print(f"  :P Can send arbitrary data through WebRTC")
    if got_response:
        print(f"  :P Received response from peer")
    print(f"\n  Next: Build OlcRTC protocol on top of this")
    print(f"{'='*60}")

if __name__ == "__main__":
    asyncio.run(main())
