#!/usr/bin/env python3

import asyncio
import json
import uuid
import sys
import websockets
import requests
from urllib.parse import quote
from aiortc import RTCPeerConnection, RTCSessionDescription, RTCIceCandidate, RTCConfiguration, RTCIceServer

CONFERENCE_ID = "46984088311346"
CONFERENCE_URL = f"https://telemost.yandex.ru/j/{CONFERENCE_ID}"
API_BASE = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"

def generate_uuid():
    return str(uuid.uuid4())

def get_connection_info(display_name):
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

async def run_peer(name, is_server=False):
    conn_info = get_connection_info(name)
    room_id = conn_info["room_id"]
    peer_id = conn_info["peer_id"]
    credentials = conn_info["credentials"]
    ws_url = conn_info["client_configuration"]["media_server_url"]
    
    print(f"[{name}] Room: {room_id}")
    print(f"[{name}] Peer: {peer_id}\n")
    
    pc_sub = RTCPeerConnection(RTCConfiguration(
        iceServers=[RTCIceServer(urls=["stun:stun.rtc.yandex.net:3478"])]
    ))
    
    pc_pub = RTCPeerConnection(RTCConfiguration(
        iceServers=[RTCIceServer(urls=["stun:stun.rtc.yandex.net:3478"])]
    ))
    
    dc_pub = pc_pub.createDataChannel("olcrtc", ordered=True)
    dc_pub_open = asyncio.Event()
    dc_sub_channels = {}
    
    @dc_pub.on("open")
    def on_pub_open():
        print(f"[{name}] :P Publisher DC opened")
        dc_pub_open.set()
    
    @dc_pub.on("message")
    def on_pub_msg(msg):
        print(f"[{name}] <PUB> {msg[:80]}{'...' if len(msg) > 80 else ''}")
    
    @pc_sub.on("datachannel")
    def on_sub_dc(channel):
        print(f"[{name}] + Subscriber DC: {channel.label}")
        dc_sub_channels[channel.label] = channel
        
        @channel.on("open")
        def on_open():
            print(f"[{name}] :P Subscriber DC '{channel.label}' opened")
        
        @channel.on("message")
        def on_message(message):
            print(f"[{name}] <SUB> {message[:80]}{'...' if len(message) > 80 else ''} ({len(message)}b)")
            
            if is_server:
                response = f"Echo: {message}"
                try:
                    dc_pub.send(response)
                    print(f"[{name}] >PUB> Echo sent")
                except Exception as e:
                    print(f"[{name}] ! Send error: {e}")
    
    async with websockets.connect(ws_url) as ws:
        hello_msg = {
            "uid": generate_uuid(),
            "hello": {
                "participantMeta": {"name": name, "role": "SPEAKER", "sendAudio": False, "sendVideo": False},
                "participantAttributes": {"name": name, "role": "SPEAKER"},
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
                    "dataChannelSharing": ["TO_RTP"]
                },
                "sdkInfo": {"implementation": "python", "version": "1.0.0", "userAgent": f"OlcRTC-{name}"},
                "sdkInitializationId": generate_uuid(),
                "disablePublisher": False,
                "disableSubscriber": False
            }
        }
        
        await ws.send(json.dumps(hello_msg))
        
        publisher_sdp_sent = False
        
        async def ws_handler():
            nonlocal publisher_sdp_sent
            while True:
                try:
                    data = json.loads(await ws.recv())
                    msg_types = [k for k in data.keys() if k != 'uid']
                    
                    if "ack" not in msg_types and "webrtcIceCandidate" not in msg_types:
                        print(f"[{name}] < {msg_types}")
                    
                    if "serverHello" in data:
                        await ws.send(json.dumps({"uid": data["uid"], "ack": {"status": {"code": "OK"}}}))
                    
                    if "subscriberSdpOffer" in data and not publisher_sdp_sent:
                        await pc_sub.setRemoteDescription(RTCSessionDescription(
                            sdp=data["subscriberSdpOffer"]["sdp"], type="offer"
                        ))
                        
                        answer = await pc_sub.createAnswer()
                        await pc_sub.setLocalDescription(answer)
                        
                        await ws.send(json.dumps({
                            "uid": generate_uuid(),
                            "subscriberSdpAnswer": {
                                "pcSeq": data["subscriberSdpOffer"]["pcSeq"],
                                "sdp": pc_sub.localDescription.sdp
                            }
                        }))
                        
                        await ws.send(json.dumps({"uid": data["uid"], "ack": {"status": {"code": "OK"}}}))
                        
                        await asyncio.sleep(0.5)
                        
                        pub_offer = await pc_pub.createOffer()
                        await pc_pub.setLocalDescription(pub_offer)
                        
                        await ws.send(json.dumps({
                            "uid": generate_uuid(),
                            "publisherSdpOffer": {
                                "pcSeq": 1,
                                "sdp": pc_pub.localDescription.sdp
                            }
                        }))
                        print(f"[{name}] > Sent publisher offer")
                        publisher_sdp_sent = True
                    
                    if "publisherSdpAnswer" in data:
                        await pc_pub.setRemoteDescription(RTCSessionDescription(
                            sdp=data["publisherSdpAnswer"]["sdp"], type="answer"
                        ))
                        print(f"[{name}] + Got publisher answer")
                        await ws.send(json.dumps({"uid": data["uid"], "ack": {"status": {"code": "OK"}}}))
                    
                    if "webrtcIceCandidate" in data:
                        cand = data["webrtcIceCandidate"]
                        try:
                            parts = cand["candidate"].split()
                            if len(parts) >= 8:
                                ice = RTCIceCandidate(
                                    component=int(parts[1]),
                                    foundation=parts[0].replace("candidate:", ""),
                                    ip=parts[4],
                                    port=int(parts[5]),
                                    priority=int(parts[3]),
                                    protocol=parts[2],
                                    type=parts[7],
                                    sdpMid=cand["sdpMid"],
                                    sdpMLineIndex=cand["sdpMlineIndex"]
                                )
                                
                                if cand.get("target") == "SUBSCRIBER":
                                    await pc_sub.addIceCandidate(ice)
                                elif cand.get("target") == "PUBLISHER":
                                    await pc_pub.addIceCandidate(ice)
                        except:
                            pass
                
                except Exception as e:
                    print(f"[{name}] ! WS error: {e}")
                    break
        
        @pc_sub.on("icecandidate")
        async def on_sub_ice(event):
            if event.candidate:
                await ws.send(json.dumps({
                    "uid": generate_uuid(),
                    "webrtcIceCandidate": {
                        "candidate": event.candidate.candidate,
                        "sdpMid": event.candidate.sdpMid,
                        "sdpMlineIndex": event.candidate.sdpMLineIndex,
                        "target": "SUBSCRIBER",
                        "pcSeq": 1
                    }
                }))
        
        @pc_pub.on("icecandidate")
        async def on_pub_ice(event):
            if event.candidate:
                await ws.send(json.dumps({
                    "uid": generate_uuid(),
                    "webrtcIceCandidate": {
                        "candidate": event.candidate.candidate,
                        "sdpMid": event.candidate.sdpMid,
                        "sdpMlineIndex": event.candidate.sdpMLineIndex,
                        "target": "PUBLISHER",
                        "pcSeq": 1
                    }
                }))
        
        ws_task = asyncio.create_task(ws_handler())
        
        try:
            await asyncio.wait_for(dc_pub_open.wait(), timeout=15.0)
            
            if is_server:
                print(f"[{name}] * Server mode - listening...\n")
                while True:
                    await asyncio.sleep(1)
            else:
                print(f"[{name}] * Client mode - sending messages...\n")
                await asyncio.sleep(2)
                
                for i in range(5):
                    msg = f"Message {i+1} from {name}"
                    dc_pub.send(msg)
                    print(f"[{name}] >PUB> {msg}")
                    await asyncio.sleep(1)
                
                print(f"[{name}] * Done, waiting 5s...")
                await asyncio.sleep(5)
        
        except asyncio.TimeoutError:
            print(f"[{name}] ! Timeout")
        except KeyboardInterrupt:
            print(f"\n[{name}] * Stopping...")
        finally:
            ws_task.cancel()
            await pc_sub.close()
            await pc_pub.close()

async def main():
    if len(sys.argv) < 2:
        print("Usage: python3 telemost_peer.py <server|client>")
        sys.exit(1)
    
    mode = sys.argv[1]
    is_server = mode == "server"
    name = "Server" if is_server else "Client"
    
    print(f"{'='*60}")
    print(f"  OlcRTC {name} - Yandex Telemost SFU")
    print(f"{'='*60}\n")
    
    await run_peer(name, is_server)

if __name__ == "__main__":
    asyncio.run(main())
