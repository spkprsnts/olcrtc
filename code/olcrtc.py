#!/usr/bin/env python3

# ===========================================
# AI GENERATED / AI GENERATED / AI GENERATED
# ===========================================


import asyncio
import json
import uuid
import struct
import socket
import logging
from urllib.parse import quote
import websockets
import requests
from aiortc import RTCPeerConnection, RTCSessionDescription, RTCIceCandidate, RTCConfiguration, RTCIceServer
from cryptography.hazmat.primitives.ciphers.aead import ChaCha20Poly1305
import os

logging.basicConfig(level=logging.INFO, format='[%(levelname)s] %(message)s')
log = logging.getLogger(__name__)

logging.getLogger('aiortc').setLevel(logging.ERROR)
logging.getLogger('aioice').setLevel(logging.ERROR)
logging.getLogger('av').setLevel(logging.ERROR)

API_BASE = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"
CHUNK_SIZE = 7168
BUFFER_THRESHOLD = 16384

def gen_uuid():
    return str(uuid.uuid4())

def get_connection_info(room_url, display_name):
    url = f"{API_BASE}/conferences/{quote(room_url, safe='')}/connection"
    params = {
        "next_gen_media_platform_allowed": "true",
        "display_name": display_name,
        "waiting_room_supported": "true"
    }
    headers = {
        "User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:149.0) Gecko/20100101 Firefox/149.0",
        "Accept": "*/*",
        "content-type": "application/json",
        "Client-Instance-Id": gen_uuid(),
        "X-Telemost-Client-Version": "187.1.0",
        "idempotency-key": gen_uuid(),
        "Origin": "https://telemost.yandex.ru",
        "Referer": "https://telemost.yandex.ru/"
    }
    r = requests.get(url, params=params, headers=headers)
    r.raise_for_status()
    return r.json()

class Crypto:
    def __init__(self, key):
        self.cipher = ChaCha20Poly1305(key)
    
    def encrypt(self, data):
        nonce = os.urandom(12)
        ct = self.cipher.encrypt(nonce, data, None)
        return nonce + ct
    
    def decrypt(self, blob):
        nonce = blob[:12]
        ct = blob[12:]
        return self.cipher.decrypt(nonce, ct, None)

class Multiplexer:
    def __init__(self, on_send):
        self.streams = {}
        self.next_id = 1
        self.on_send = on_send
    
    def open_stream(self):
        sid = self.next_id
        self.next_id += 1
        self.streams[sid] = {
            "recv_buf": b"",
            "send_queue": asyncio.Queue(),
            "closed": False
        }
        return sid
    
    def close_stream(self, sid):
        if sid in self.streams:
            self.streams[sid]["closed"] = True
    
    async def send_data(self, sid, data):
        if sid not in self.streams or self.streams[sid]["closed"]:
            return
        
        log.debug(f"MUX send sid={sid} len={len(data)}")
        for i in range(0, len(data), CHUNK_SIZE):
            chunk = data[i:i+CHUNK_SIZE]
            frame = struct.pack("!HH", sid, len(chunk)) + chunk
            await self.on_send(frame)
    
    async def send_close(self, sid):
        frame = struct.pack("!HH", sid, 0)
        await self.on_send(frame)
        self.close_stream(sid)
    
    def handle_frame(self, frame):
        if len(frame) < 4:
            log.warning(f"MUX frame too short: {len(frame)}b")
            return
        
        sid, length = struct.unpack("!HH", frame[:4])
        
        if length == 0:
            log.debug(f"MUX close sid={sid}")
            self.close_stream(sid)
            return
        
        data = frame[4:4+length]
        
        if sid not in self.streams:
            log.warning(f"MUX recv sid={sid} not found, opening it")
            self.streams[sid] = {
                "recv_buf": b"",
                "send_queue": asyncio.Queue(),
                "closed": False
            }
        
        self.streams[sid]["recv_buf"] += data
        log.debug(f"MUX recv sid={sid} len={len(data)} total_buf={len(self.streams[sid]['recv_buf'])}")
    
    def read_stream(self, sid, max_n=None):
        if sid not in self.streams:
            return b""
        
        buf = self.streams[sid]["recv_buf"]
        if not buf:
            return b""
        
        if max_n is None:
            result = buf
            self.streams[sid]["recv_buf"] = b""
        else:
            result = buf[:max_n]
            self.streams[sid]["recv_buf"] = buf[max_n:]
        
        return result
    
    def stream_closed(self, sid):
        return sid not in self.streams or self.streams[sid]["closed"]

class RTCPeer:
    def __init__(self, room_url, name, crypto):
        self.room_url = room_url
        self.name = name
        self.crypto = crypto
        self.dc = None
        self.dc_ready = asyncio.Event()
        self.mux = None
        
    async def connect(self):
        conn = get_connection_info(self.room_url, self.name)
        room_id = conn["room_id"]
        peer_id = conn["peer_id"]
        credentials = conn["credentials"]
        ws_url = conn["client_configuration"]["media_server_url"]
        
        pc_sub = RTCPeerConnection(RTCConfiguration(
            iceServers=[RTCIceServer(urls=["stun:stun.rtc.yandex.net:3478"])]
        ))
        pc_pub = RTCPeerConnection(RTCConfiguration(
            iceServers=[RTCIceServer(urls=["stun:stun.rtc.yandex.net:3478"])]
        ))
        
        self.dc = pc_pub.createDataChannel("olcrtc", ordered=True)
        
        @self.dc.on("open")
        def on_open():
            self.dc_ready.set()
        
        @self.dc.on("message")
        def on_msg(msg):
            if isinstance(msg, bytes):
                try:
                    plain = self.crypto.decrypt(msg)
                    self.mux.handle_frame(plain)
                    log.debug(f"DC received {len(msg)}b encrypted, {len(plain)}b plain")
                except Exception as e:
                    log.error(f"DC decrypt error: {e}")
        
        @pc_sub.on("datachannel")
        def on_dc(ch):
            log.info(f"Received datachannel: {ch.label}")
            @ch.on("message")
            def on_message(msg):
                if isinstance(msg, bytes):
                    try:
                        plain = self.crypto.decrypt(msg)
                        self.mux.handle_frame(plain)
                        log.debug(f"SUB DC received {len(msg)}b encrypted, {len(plain)}b plain")
                    except Exception as e:
                        log.error(f"SUB DC decrypt error: {e}")
        
        ws = await websockets.connect(ws_url)
        
        hello = {
            "uid": gen_uuid(),
            "hello": {
                "participantMeta": {"name": self.name, "role": "SPEAKER", "sendAudio": False, "sendVideo": False},
                "participantAttributes": {"name": self.name, "role": "SPEAKER"},
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
                "sdkInfo": {"implementation": "python", "version": "1.0.0", "userAgent": f"OlcRTC-{self.name}"},
                "sdkInitializationId": gen_uuid(),
                "disablePublisher": False,
                "disableSubscriber": False
            }
        }
        
        await ws.send(json.dumps(hello))
        
        pub_sent = False
        
        async def ws_loop():
            nonlocal pub_sent
            while True:
                try:
                    msg = json.loads(await ws.recv())
                    
                    if "serverHello" in msg:
                        await ws.send(json.dumps({"uid": msg["uid"], "ack": {"status": {"code": "OK"}}}))
                    
                    if "subscriberSdpOffer" in msg and not pub_sent:
                        await pc_sub.setRemoteDescription(RTCSessionDescription(
                            sdp=msg["subscriberSdpOffer"]["sdp"], type="offer"
                        ))
                        ans = await pc_sub.createAnswer()
                        await pc_sub.setLocalDescription(ans)
                        await ws.send(json.dumps({
                            "uid": gen_uuid(),
                            "subscriberSdpAnswer": {
                                "pcSeq": msg["subscriberSdpOffer"]["pcSeq"],
                                "sdp": pc_sub.localDescription.sdp
                            }
                        }))
                        await ws.send(json.dumps({"uid": msg["uid"], "ack": {"status": {"code": "OK"}}}))
                        await asyncio.sleep(0.3)
                        
                        offer = await pc_pub.createOffer()
                        await pc_pub.setLocalDescription(offer)
                        await ws.send(json.dumps({
                            "uid": gen_uuid(),
                            "publisherSdpOffer": {
                                "pcSeq": 1,
                                "sdp": pc_pub.localDescription.sdp
                            }
                        }))
                        pub_sent = True
                    
                    if "publisherSdpAnswer" in msg:
                        await pc_pub.setRemoteDescription(RTCSessionDescription(
                            sdp=msg["publisherSdpAnswer"]["sdp"], type="answer"
                        ))
                        await ws.send(json.dumps({"uid": msg["uid"], "ack": {"status": {"code": "OK"}}}))
                    
                    if "webrtcIceCandidate" in msg:
                        cand = msg["webrtcIceCandidate"]
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
                except:
                    break
        
        @pc_sub.on("icecandidate")
        async def on_sub_ice(e):
            if e.candidate:
                await ws.send(json.dumps({
                    "uid": gen_uuid(),
                    "webrtcIceCandidate": {
                        "candidate": e.candidate.candidate,
                        "sdpMid": e.candidate.sdpMid,
                        "sdpMlineIndex": e.candidate.sdpMLineIndex,
                        "target": "SUBSCRIBER",
                        "pcSeq": 1
                    }
                }))
        
        @pc_pub.on("icecandidate")
        async def on_pub_ice(e):
            if e.candidate:
                await ws.send(json.dumps({
                    "uid": gen_uuid(),
                    "webrtcIceCandidate": {
                        "candidate": e.candidate.candidate,
                        "sdpMid": e.candidate.sdpMid,
                        "sdpMlineIndex": e.candidate.sdpMLineIndex,
                        "target": "PUBLISHER",
                        "pcSeq": 1
                    }
                }))
        
        asyncio.create_task(ws_loop())
        
        async def send_encrypted(data):
            enc = self.crypto.encrypt(data)
            while self.dc.bufferedAmount > BUFFER_THRESHOLD:
                await asyncio.sleep(0.001)
            self.dc.send(enc)
            log.debug(f"DC sent {len(data)}b plain, {len(enc)}b encrypted")
        
        self.mux = Multiplexer(send_encrypted)
        
        await asyncio.wait_for(self.dc_ready.wait(), timeout=15.0)

class SOCKS5Server:
    def __init__(self, peer, host="127.0.0.1", port=1080):
        self.peer = peer
        self.host = host
        self.port = port
    
    async def handle_client(self, reader, writer):
        sid = None
        try:
            ver = await reader.readexactly(1)
            if ver[0] != 5:
                writer.close()
                return
            
            nmethods = await reader.readexactly(1)
            await reader.readexactly(nmethods[0])
            
            writer.write(b"\x05\x00")
            await writer.drain()
            
            req = await reader.readexactly(4)
            if req[1] != 1:
                writer.write(b"\x05\x07\x00\x01\x00\x00\x00\x00\x00\x00")
                await writer.drain()
                writer.close()
                return
            
            atyp = req[3]
            if atyp == 1:
                addr = socket.inet_ntoa(await reader.readexactly(4))
            elif atyp == 3:
                length = (await reader.readexactly(1))[0]
                addr = (await reader.readexactly(length)).decode()
            else:
                writer.write(b"\x05\x08\x00\x01\x00\x00\x00\x00\x00\x00")
                await writer.drain()
                writer.close()
                return
            
            port_bytes = await reader.readexactly(2)
            port = struct.unpack("!H", port_bytes)[0]
            
            sid = self.peer.mux.open_stream()
            log.info(f"SOCKS5 connect sid={sid} {addr}:{port}")
            
            connect_req = json.dumps({"cmd": "connect", "addr": addr, "port": port}).encode()
            await self.peer.mux.send_data(sid, connect_req)
            
            await asyncio.sleep(0.5)
            
            writer.write(b"\x05\x00\x00\x01\x00\x00\x00\x00\x00\x00")
            await writer.drain()
            
            async def client_to_stream():
                try:
                    while True:
                        data = await reader.read(4096)
                        if not data:
                            break
                        log.debug(f"SOCKS5 sid={sid} client->stream {len(data)}b")
                        await self.peer.mux.send_data(sid, data)
                    await self.peer.mux.send_close(sid)
                    log.debug(f"SOCKS5 sid={sid} client closed")
                except Exception as e:
                    log.error(f"SOCKS5 sid={sid} client_to_stream error: {e}")
            
            async def stream_to_client():
                try:
                    while not self.peer.mux.stream_closed(sid):
                        await asyncio.sleep(0.01)
                        data = self.peer.mux.read_stream(sid)
                        if data:
                            log.debug(f"SOCKS5 sid={sid} stream->client {len(data)}b")
                            writer.write(data)
                            await writer.drain()
                    log.debug(f"SOCKS5 sid={sid} stream closed")
                except Exception as e:
                    log.error(f"SOCKS5 sid={sid} stream_to_client error: {e}")
            
            await asyncio.gather(client_to_stream(), stream_to_client())
            
        except Exception as e:
            log.error(f"SOCKS5 sid={sid} error: {e}")
        finally:
            try:
                writer.close()
                await writer.wait_closed()
            except:
                pass
    
    async def run(self):
        server = await asyncio.start_server(self.handle_client, self.host, self.port)
        print(f"SOCKS5 proxy listening on {self.host}:{self.port}")
        async with server:
            await server.serve_forever()

class ProxyServer:
    def __init__(self, peer):
        self.peer = peer
        self.connections = {}
    
    async def handle_stream(self, sid, req):
        try:
            cmd = req.get("cmd")
            if cmd == "connect":
                addr = req["addr"]
                port = req["port"]
                
                log.info(f"SERVER connect sid={sid} {addr}:{port}")
                
                try:
                    r, w = await asyncio.open_connection(addr, port)
                    self.connections[sid] = (r, w)
                    log.info(f"SERVER sid={sid} connected")
                    
                    async def remote_to_stream():
                        try:
                            while True:
                                data = await r.read(4096)
                                if not data:
                                    break
                                log.debug(f"SERVER sid={sid} remote->stream {len(data)}b")
                                await self.peer.mux.send_data(sid, data)
                            await self.peer.mux.send_close(sid)
                            log.debug(f"SERVER sid={sid} remote closed")
                        except Exception as e:
                            log.error(f"SERVER sid={sid} remote_to_stream error: {e}")
                    
                    asyncio.create_task(remote_to_stream())
                    
                except Exception as e:
                    log.error(f"SERVER sid={sid} connect failed: {e}")
                    await self.peer.mux.send_close(sid)
        except Exception as e:
            log.error(f"SERVER sid={sid} handle_stream error: {e}")
    
    async def run(self):
        log.info("SERVER proxy loop started")
        while True:
            await asyncio.sleep(0.01)
            for sid in list(self.peer.mux.streams.keys()):
                data = self.peer.mux.read_stream(sid)
                if data:
                    if sid in self.connections:
                        r, w = self.connections[sid]
                        try:
                            log.debug(f"SERVER sid={sid} stream->remote {len(data)}b")
                            w.write(data)
                            await w.drain()
                        except Exception as e:
                            log.error(f"SERVER sid={sid} write error: {e}")
                            await self.peer.mux.send_close(sid)
                    else:
                        try:
                            req = json.loads(data.decode())
                            await self.handle_stream(sid, req)
                        except Exception as e:
                            log.error(f"SERVER sid={sid} parse error: {e}")
                
                if self.peer.mux.stream_closed(sid) and sid in self.connections:
                    log.debug(f"SERVER sid={sid} cleanup")
                    r, w = self.connections[sid]
                    try:
                        w.close()
                        await w.wait_closed()
                    except:
                        pass
                    del self.connections[sid]

async def run_server(room_url, key):
    crypto = Crypto(key)
    peer = RTCPeer(room_url, "OlcRTC-Server", crypto)
    
    log.info("Connecting to Telemost...")
    await peer.connect()
    log.info("Connected to Telemost")
    
    proxy = ProxyServer(peer)
    await proxy.run()

async def run_client(room_url, key, socks_port):
    crypto = Crypto(key)
    peer = RTCPeer(room_url, "OlcRTC-Client", crypto)
    
    log.info("Connecting to Telemost...")
    await peer.connect()
    log.info("Connected to Telemost")
    
    socks = SOCKS5Server(peer, port=socks_port)
    await socks.run()

def main():
    import argparse
    
    parser = argparse.ArgumentParser(description="OlcRTC - SOCKS5 over WebRTC DataChannel")
    parser.add_argument("--srv", action="store_true", help="Run as server")
    parser.add_argument("--cnc", action="store_true", help="Run as client")
    parser.add_argument("--id", required=True, help="Telemost room ID")
    parser.add_argument("--provider", default="telemost", help="Provider (telemost only)")
    parser.add_argument("--socks-port", type=int, default=1080, help="SOCKS5 port (client only)")
    parser.add_argument("--key", help="Shared encryption key (hex)")
    parser.add_argument("--debug", action="store_true", help="Enable debug logging")
    
    args = parser.parse_args()
    
    if args.debug:
        logging.getLogger().setLevel(logging.DEBUG)
    
    if args.provider != "telemost":
        log.error("Only telemost provider supported in MVP")
        return
    
    room_url = f"https://telemost.yandex.ru/j/{args.id}"
    
    if args.key:
        key = bytes.fromhex(args.key)
    else:
        key = os.urandom(32)
        log.info(f"Generated key: {key.hex()}")
    
    if args.srv:
        log.info(f"Starting server mode, room: {args.id}")
        asyncio.run(run_server(room_url, key))
    elif args.cnc:
        log.info(f"Starting client mode, room: {args.id}, SOCKS5 port: {args.socks_port}")
        asyncio.run(run_client(room_url, key, args.socks_port))
    else:
        log.error("Specify --srv or --cnc")

if __name__ == "__main__":
    main()
