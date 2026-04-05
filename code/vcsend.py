#!/usr/bin/env python3

import asyncio
import json
import uuid
import websockets
import requests
import qrcode
import cv2
import numpy as np
from urllib.parse import quote
from aiortc import RTCPeerConnection, RTCSessionDescription, RTCIceCandidate, RTCConfiguration, RTCIceServer
from aiortc.mediastreams import MediaStreamTrack
from av import VideoFrame
import base64
from PIL import Image
from pyzbar import pyzbar
from fractions import Fraction

CONFERENCE_ID = "75047680642749"
CONFERENCE_URL = f"https://telemost.yandex.ru/j/{CONFERENCE_ID}"
API_BASE = "https://cloud-api.yandex.ru/telemost_front/v2/telemost"

QR_SIZE = 600
CHUNK_SIZE = 400
FRAME_RATE = 1


def gen_uid():
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
        "Client-Instance-Id": gen_uid(),
        "X-Telemost-Client-Version": "187.1.0",
        "idempotency-key": gen_uid(),
        "Origin": "https://telemost.yandex.ru",
        "Referer": "https://telemost.yandex.ru/"
    }
    r = requests.get(url, params=params, headers=headers)
    r.raise_for_status()
    return r.json()


def make_qr_frame(data, pts):
    qr = qrcode.QRCode(
        version=None,
        error_correction=qrcode.constants.ERROR_CORRECT_M,
        box_size=12,
        border=4
    )
    qr.add_data(data)
    qr.make(fit=True)
    img = qr.make_image(fill_color="black", back_color="white").resize(
        (QR_SIZE, QR_SIZE), Image.NEAREST
    )
    arr = np.array(img.convert('RGB'))
    frame = VideoFrame.from_ndarray(arr, format="rgb24")
    frame.pts = pts
    frame.time_base = Fraction(1, FRAME_RATE)
    return frame


def chunk_data(data, tid):
    b64 = base64.b64encode(data).decode()
    n = (len(b64) + CHUNK_SIZE - 1) // CHUNK_SIZE
    return [json.dumps({"tid": tid, "idx": i, "total": n,
                        "data": b64[i * CHUNK_SIZE:(i + 1) * CHUNK_SIZE]})
            for i in range(n)]


class QRVideoTrack(MediaStreamTrack):
    kind = "video"

    def __init__(self):
        super().__init__()
        self._frames = []
        self._idx = 0
        self._pts = 0

    def set_data(self, chunks):
        self._frames = [make_qr_frame(c, i) for i, c in enumerate(chunks)]
        self._idx = 0
        self._pts = 0
        print(f"      -> QRVideoTrack: {len(self._frames)} frames ready")

    async def recv(self):
        await asyncio.sleep(1.0 / FRAME_RATE)
        if not self._frames:
            f = make_qr_frame("WAIT", self._pts)
            self._pts += 1
            return f
        f = self._frames[self._idx]
        f.pts = self._pts
        f.time_base = Fraction(1, FRAME_RATE)
        self._pts += 1
        self._idx = (self._idx + 1) % len(self._frames)
        if self._pts % 10 == 0:
            print(f"      -> QR sending frame {self._idx}/{len(self._frames)} pts={self._pts}")
        return f


class QRReceiver:
    def __init__(self):
        self._bufs = {}
        self.result = None
        self._frame_count = 0
        self._cv2_detector = cv2.QRCodeDetector()

    def feed_frame(self, frame):
        try:
            self._frame_count += 1
            arr = frame.to_ndarray(format="rgb24")
            h, w = arr.shape[:2]

            if self._frame_count <= 3:
                cv2.imwrite(f"/tmp/qr_recv_{self._frame_count}.png",
                            cv2.cvtColor(arr, cv2.COLOR_RGB2BGR))
                print(f"      -> [recv] saved /tmp/qr_recv_{self._frame_count}.png {w}x{h}")

            gray = cv2.cvtColor(arr, cv2.COLOR_RGB2GRAY)

            variants = [gray]
            up2 = cv2.resize(gray, (w * 2, h * 2), interpolation=cv2.INTER_CUBIC)
            variants.append(up2)
            _, thresh = cv2.threshold(gray, 0, 255, cv2.THRESH_BINARY + cv2.THRESH_OTSU)
            variants.append(thresh)
            variants.append(cv2.resize(thresh, (w * 2, h * 2), interpolation=cv2.INTER_NEAREST))

            decoded = set()
            for v in variants:
                try:
                    for code in pyzbar.decode(v):
                        decoded.add(code.data.decode('utf-8'))
                except Exception:
                    pass
                try:
                    val, _, _ = self._cv2_detector.detectAndDecode(v)
                    if val:
                        decoded.add(val)
                except Exception:
                    pass

            for raw in decoded:
                try:
                    pkt = json.loads(raw)
                    tid = pkt["tid"]
                    idx = pkt["idx"]
                    total = pkt["total"]
                    data = pkt["data"]
                    if tid not in self._bufs:
                        self._bufs[tid] = {}
                    if idx not in self._bufs[tid]:
                        self._bufs[tid][idx] = data
                        print(f"      -> QR chunk {idx+1}/{total} tid={tid[:8]}")
                    if len(self._bufs[tid]) == total:
                        b64 = "".join(self._bufs[tid][i] for i in range(total))
                        self.result = base64.b64decode(b64)
                        print(f"      -> QR COMPLETE: {len(self.result)} bytes")
                        return True
                except Exception:
                    pass

            if self._frame_count % 30 == 0:
                print(f"      -> [recv] {self._frame_count} frames, no QR, size={w}x{h}")

        except Exception as e:
            print(f"      -> [recv] feed_frame err: {e}")
        return False


async def process_track(track, receiver, name):
    print(f"      -> [{name}] video processor started")
    count = 0
    while True:
        try:
            frame = await asyncio.wait_for(track.recv(), timeout=30.0)
            count += 1
            if count <= 5 or count % 50 == 0:
                print(f"      -> [{name}] frame #{count} {frame.width}x{frame.height}")
            if receiver.feed_frame(frame):
                return
        except asyncio.TimeoutError:
            print(f"      -> [{name}] track frozen after {count} frames")
            return
        except Exception as e:
            print(f"      -> [{name}] track err: {e}")
            return


def make_ice_servers(raw_list):
    result = []
    for s in raw_list:
        urls = s.get("urls", [])
        cred = s.get("credential", "")
        user = s.get("username", "")
        if cred:
            result.append(RTCIceServer(urls=urls, credential=cred, username=user))
        else:
            result.append(RTCIceServer(urls=urls))
    return result or [RTCIceServer(urls=["stun:stun.rtc.yandex.net:3478"])]


async def connect_peer(name, conn):
    room_id = conn["room_id"]
    peer_id = conn["peer_id"]
    credentials = conn["credentials"]
    ws_url = conn["client_configuration"]["media_server_url"]
    is_sender = "Sender" in name

    print(f"\n      -> [{name}] room={room_id} peer={peer_id[:8]} sender={is_sender}")

    default_ice = [RTCIceServer(urls=["stun:stun.rtc.yandex.net:3478"])]

    video_track = QRVideoTrack() if is_sender else None
    receiver_obj = QRReceiver() if not is_sender else None
    track_tasks = []

    # используем списки чтобы можно было переприсваивать в замыканиях
    pc_sub_ref = [RTCPeerConnection(RTCConfiguration(iceServers=default_ice))]
    pc_pub_ref = [RTCPeerConnection(RTCConfiguration(iceServers=default_ice))]

    if is_sender:
        pc_pub_ref[0].addTrack(video_track)

    ws = await websockets.connect(
        ws_url,
        additional_headers={
            "Origin": "https://telemost.yandex.ru",
            "User-Agent": "Mozilla/5.0 (X11; Linux x86_64; rv:149.0) Gecko/20100101 Firefox/149.0"
        }
    )
    print(f"      -> [{name}] WS connected")

    async def send(obj):
        await ws.send(json.dumps(obj))

    async def ack(uid):
        await send({"uid": uid, "ack": {"status": {"code": "OK", "description": ""}}})

    def setup_pc(pc_sub, pc_pub):
        @pc_sub.on("track")
        def on_track(track):
            print(f"      -> [{name}] GOT TRACK kind={track.kind}")
            if track.kind == "video" and receiver_obj is not None:
                t = asyncio.ensure_future(process_track(track, receiver_obj, name))
                track_tasks.append(t)

        @pc_sub.on("connectionstatechange")
        async def _s():
            print(f"      -> [{name}] sub={pc_sub.connectionState}")

        @pc_pub.on("connectionstatechange")
        async def _p():
            print(f"      -> [{name}] pub={pc_pub.connectionState}")

        @pc_sub.on("iceconnectionstatechange")
        async def _si():
            print(f"      -> [{name}] sub ICE={pc_sub.iceConnectionState}")

        @pc_pub.on("iceconnectionstatechange")
        async def _pi():
            print(f"      -> [{name}] pub ICE={pc_pub.iceConnectionState}")

        @pc_sub.on("icecandidate")
        async def on_sub_ice(e):
            if e.candidate:
                await send({
                    "uid": gen_uid(),
                    "webrtcIceCandidate": {
                        "candidate": e.candidate.candidate,
                        "sdpMid": e.candidate.sdpMid,
                        "sdpMlineIndex": e.candidate.sdpMLineIndex,
                        "usernameFragment": "",
                        "target": "SUBSCRIBER",
                        "pcSeq": 1
                    }
                })
                print(f"      -> [{name}] >> sub ICE sent")

        @pc_pub.on("icecandidate")
        async def on_pub_ice(e):
            if e.candidate:
                await send({
                    "uid": gen_uid(),
                    "webrtcIceCandidate": {
                        "candidate": e.candidate.candidate,
                        "sdpMid": e.candidate.sdpMid,
                        "sdpMlineIndex": e.candidate.sdpMLineIndex,
                        "usernameFragment": "",
                        "target": "PUBLISHER",
                        "pcSeq": 1
                    }
                })
                print(f"      -> [{name}] >> pub ICE sent")

    setup_pc(pc_sub_ref[0], pc_pub_ref[0])

    hello = {
        "uid": gen_uid(),
        "hello": {
            "participantMeta": {
                "name": name, "role": "SPEAKER", "description": "",
                "sendAudio": False, "sendVideo": is_sender
            },
            "participantAttributes": {"name": name, "role": "SPEAKER", "description": ""},
            "sendAudio": False,
            "sendVideo": is_sender,
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
            "sdkInitializationId": gen_uid(),
            "disablePublisher": not is_sender,
            "disableSubscriber": False,
            "disableSubscriberAudio": True
        }
    }

    await send(hello)
    print(f"      -> [{name}] hello sent")

    pub_sdp_sent = False

    async def ws_loop():
        nonlocal pub_sdp_sent

        try:
            async for raw in ws:
                msg = json.loads(raw)
                keys = [k for k in msg if k != "uid"]
                if not keys:
                    continue
                mtype = keys[0]
                uid = msg.get("uid", "")
                print(f"      -> [{name}] << {mtype}")

                if mtype == "ack":
                    pass

                elif mtype == "serverHello":
                    sh = msg["serverHello"]
                    raw_ice = sh.get("rtcConfiguration", {}).get("iceServers", [])
                    if raw_ice:
                        ice = make_ice_servers(raw_ice)
                        old_sub = pc_sub_ref[0]
                        old_pub = pc_pub_ref[0]
                        pc_sub_ref[0] = RTCPeerConnection(RTCConfiguration(iceServers=ice))
                        pc_pub_ref[0] = RTCPeerConnection(RTCConfiguration(iceServers=ice))
                        if is_sender and video_track:
                            pc_pub_ref[0].addTrack(video_track)
                        setup_pc(pc_sub_ref[0], pc_pub_ref[0])
                        await old_sub.close()
                        await old_pub.close()
                        print(f"      -> [{name}] PC recreated with TURN")
                    await ack(uid)

                elif mtype == "subscriberSdpOffer":
                    offer_sdp = msg["subscriberSdpOffer"]["sdp"]
                    pc_seq = msg["subscriberSdpOffer"]["pcSeq"]
                    pc_sub = pc_sub_ref[0]
                    pc_pub = pc_pub_ref[0]

                    await pc_sub.setRemoteDescription(
                        RTCSessionDescription(sdp=offer_sdp, type="offer")
                    )
                    answer = await pc_sub.createAnswer()
                    await pc_sub.setLocalDescription(answer)

                    await send({
                        "uid": gen_uid(),
                        "subscriberSdpAnswer": {
                            "pcSeq": pc_seq,
                            "sdp": pc_sub.localDescription.sdp
                        }
                    })
                    print(f"      -> [{name}] >> subscriberSdpAnswer")
                    await ack(uid)

                    if not is_sender:
                        await send({
                            "uid": gen_uid(),
                            "setSlots": {
                                "slots": [
                                    {"width": 1280, "height": 720},
                                    {"width": 640, "height": 360}
                                ],
                                "audioSlotsCount": 0,
                                "key": 1,
                                "shutdownAllVideo": None,
                                "withSelfView": False,
                                "selfViewVisibility": "ON_LOADING_THEN_SHOW",
                                "gridConfig": {}
                            }
                        })
                        print(f"      -> [{name}] >> setSlots (запросили маршрутизацию видео у сервера!)")

                    if is_sender and not pub_sdp_sent:
                        await asyncio.sleep(0.3)
                        pub_offer = await pc_pub.createOffer()
                        await pc_pub.setLocalDescription(pub_offer)
                        tracks_info = []
                        for t in pc_pub.getTransceivers():
                            if t.sender.track:
                                tracks_info.append({
                                    "mid": t.mid,
                                    "transceiverMid": t.mid,
                                    "kind": t.sender.track.kind.upper(),
                                    "priority": 0,
                                    "label": "QRVideoTrack",
                                    "codecs": {},
                                    "groupId": 1,
                                    "description": ""
                                })
                        await send({
                            "uid": gen_uid(),
                            "publisherSdpOffer": {
                                "pcSeq": 1,
                                "sdp": pc_pub.localDescription.sdp,
                                "tracks": tracks_info
                            }
                        })
                        pub_sdp_sent = True
                        print(f"      -> [{name}] >> publisherSdpOffer")

                elif mtype == "publisherSdpAnswer":
                    await pc_pub_ref[0].setRemoteDescription(
                        RTCSessionDescription(sdp=msg["publisherSdpAnswer"]["sdp"], type="answer")
                    )
                    print(f"      -> [{name}] publisher answer set")
                    await ack(uid)

                elif mtype == "webrtcIceCandidate":
                    cand = msg["webrtcIceCandidate"]
                    candidate_str = cand.get("candidate", "")
                    target = cand.get("target", "")
                    sdp_mid = cand.get("sdpMid", "0")
                    sdp_mline = cand.get("sdpMlineIndex", 0)

                    if not candidate_str:
                        continue
                    parts = candidate_str.split()
                    if len(parts) < 8:
                        continue

                    try:
                        tcptype = None
                        if "tcptype" in parts:
                            ti = parts.index("tcptype")
                            tcptype = parts[ti + 1]

                        ice_c = RTCIceCandidate(
                            component=int(parts[1]),
                            foundation=parts[0].replace("candidate:", ""),
                            ip=parts[4],
                            port=int(parts[5]),
                            priority=int(parts[3]),
                            protocol=parts[2].lower(),
                            type=parts[7],
                            tcpType=tcptype,
                            sdpMid=sdp_mid,
                            sdpMLineIndex=sdp_mline
                        )
                        if target == "SUBSCRIBER":
                            await pc_sub_ref[0].addIceCandidate(ice_c)
                            print(f"      -> [{name}] sub ICE: {parts[2]} {parts[4]}:{parts[5]}")
                        elif target == "PUBLISHER":
                            await pc_pub_ref[0].addIceCandidate(ice_c)
                            print(f"      -> [{name}] pub ICE: {parts[2]} {parts[4]}:{parts[5]}")
                    except Exception as e:
                        print(f"      -> [{name}] ICE err: {e}")

                elif mtype in ("setSlots", "slotsConfig", "slotsMeta", "vadActivity",
                               "updateDescription", "upsertDescription", "sdkCodecsInfo",
                               "pingPong", "selfQualityReport", "upsertParticipantsQualityReport"):
                    await ack(uid)

                else:
                    print(f"      -> [{name}] unhandled: {mtype}")
                    if uid:
                        await ack(uid)

        except websockets.exceptions.ConnectionClosed as e:
            print(f"      -> [{name}] WS closed: {e}")
        except Exception as e:
            import traceback
            print(f"      -> [{name}] WS err: {e}")
            traceback.print_exc()

    ws_task = asyncio.create_task(ws_loop())

    return {
        "name": name,
        "ws": ws,
        "ws_task": ws_task,
        "pc_pub_ref": pc_pub_ref,
        "pc_sub_ref": pc_sub_ref,
        "video_track": video_track,
        "receiver": receiver_obj,
        "track_tasks": track_tasks
    }


async def run():
    print("""
                  VCSend - Video QR Transfer
           Request/Response over Yandex Telemost SFU
                    by zowue for olc
""")

    print("[0/3] Getting conference info...")
    sender_conn = get_connection_info("QR_Sender")
    receiver_conn = get_connection_info("QR_Receiver")
    print(f"      -> sender  room: {sender_conn['room_id']}")
    print(f"      -> receiver room: {receiver_conn['room_id']}")

    print("\n[1/3] Connecting sender...")
    sender = await connect_peer("QR_Sender", sender_conn)
    await asyncio.sleep(5)

    print("\n[2/3] Connecting receiver...")
    receiver = await connect_peer("QR_Receiver", receiver_conn)
    await asyncio.sleep(5)

    print("\n[3/3] Transfer...")
    url = "zarazaex.xyz/curl.txt"
    if not url.startswith("http"):
        url = "https://" + url

    print(f"      -> fetching {url}")
    resp = requests.get(url, timeout=10)
    resp.raise_for_status()
    data = resp.content
    print(f"      -> got {len(data)} bytes")

    tid = gen_uid()
    chunks = chunk_data(data, tid)
    sender["video_track"].set_data(chunks)
    print(f"      -> {len(chunks)} QR frames set, waiting for decode...")

    for i in range(300):
        await asyncio.sleep(1)
        if i % 15 == 0:
            print(f"      -> {i}s elapsed")
        if receiver["receiver"] and receiver["receiver"].result is not None:
            result = receiver["receiver"].result
            print(f"\n      :P got {len(result)} bytes\n")
            print("--- content ---")
            try:
                print(result.decode("utf-8"))
            except Exception:
                print(f"[binary {len(result)} bytes]")
            print("--- end ---\n")
            break
    else:
        print("      X timeout — check /tmp/qr_recv_*.png")

    for p in [sender, receiver]:
        p["ws_task"].cancel()
        try:
            await p["ws"].close()
        except Exception:
            pass
        try:
            await p["pc_pub_ref"][0].close()
            await p["pc_sub_ref"][0].close()
        except Exception:
            pass


if __name__ == "__main__":
    try:
        asyncio.run(run())
    except KeyboardInterrupt:
        print("\ninterrupted")