"""Local incremental ASR. Full-utterance redecoding, not a causal streaming model."""
import asyncio, collections, concurrent.futures, json, time, os
from contextlib import asynccontextmanager
from pathlib import Path
import numpy as np
from fastapi import FastAPI, WebSocket, WebSocketDisconnect
from starlette.middleware.trustedhost import TrustedHostMiddleware

ROOT=Path(__file__).parent
DATA=Path(os.environ['VOICE_INPUT_ASR_DATA'])
POOL=concurrent.futures.ThreadPoolExecutor(max_workers=1,thread_name_prefix='asr-metal')
MODEL=None
VAD=None
BUSY=False
READY=False
SR=16000
STEP=0.6
SILENCE=0.7
MAX_SEGMENT=18
MIN_SPEECH=1536  # 96 ms verified speech; no energy-only ASR fallback
SPEECH_THRESHOLD=0.5
TAIL_PAD=2048  # 128 ms after the last speech frame

def load():
 global MODEL,VAD
 from speech_gate import load_detector
 VAD=load_detector()
 from mlx_audio.stt.utils import load_model
 import mlx.core as mx
 started=time.monotonic()
 MODEL=load_model(str(DATA/'model'))
 MODEL.generate(np.zeros(SR,dtype=np.float32),language='Chinese',max_tokens=8,verbose=False)
 mx.eval(MODEL.parameters())
 print(json.dumps({'event':'loaded','seconds':time.monotonic()-started,'model':str(DATA/'model'),'memory_gb':mx.get_active_memory()/1e9}),flush=True)

DEFAULT_HOTWORDS=[]  # Personal dictionary is supplied by the app for each session.
MAX_HOTWORDS=128
MAX_HOTWORD_CHARS=4096

def transcribe(samples,hotwords):
 t=time.monotonic()
 result=MODEL.generate(samples,language='Chinese',hotwords=hotwords,max_tokens=256,temperature=0,verbose=False)
 return result.text.strip(),time.monotonic()-t

@asynccontextmanager
async def lifespan(app):
 global READY
 await asyncio.get_running_loop().run_in_executor(POOL,load)
 READY=True
 yield
 POOL.shutdown(wait=False)

app=FastAPI(lifespan=lifespan)
app.add_middleware(TrustedHostMiddleware,allowed_hosts=['127.0.0.1','localhost'])
@app.get('/health')
def health(): return {'instance':os.environ['VOICE_INPUT_INSTANCE'],'ready':READY,'busy':BUSY,'model':'Qwen3-ASR-1.7B-8bit','step_s':STEP,'silence_s':SILENCE,'mode':'incremental utterance redecoding','vad':'Silero ONNX CPU','min_speech_ms':96,'default_hotword_count':len(DEFAULT_HOTWORDS)}
@app.websocket('/ws')
async def live(ws:WebSocket):
 global BUSY
 origin=ws.headers.get('origin')
 if origin:  # Native app only; reject browser-originated microphone sessions.
  await ws.close(code=1008);return
 await ws.accept()
 if BUSY:
  await ws.send_json({'type':'error','message':'已有录音会话，请先停止另一窗口。'})
  await ws.close(code=1013);return
 raw_terms=ws.query_params.get('hotwords')
 if raw_terms is not None and len(raw_terms)>MAX_HOTWORD_CHARS:
  await ws.send_json({'type':'error','message':'术语总长度请控制在 4096 字符以内。'})
  await ws.close(code=1008);return
 hotwords=DEFAULT_HOTWORDS if raw_terms is None else [x.strip() for x in raw_terms.replace('，',',').split(',') if x.strip()]
 if len(hotwords)>MAX_HOTWORDS:
  await ws.send_json({'type':'error','message':'术语最多 128 项，请减少后重试。'})
  await ws.close(code=1008);return
 BUSY=True
 wake=asyncio.Event(); finals=collections.deque(); latest=None; stopping=False
 from speech_gate import SpeechGate
 gate=SpeechGate(VAD);pending=np.empty(0,np.float32)
 segment=0; chunks=[]; n=0; silence=0; since=0; speech_n=0; pre=np.empty(0,np.float32)
 started=time.monotonic()
 async def decode_loop():
  nonlocal latest
  while True:
   await wake.wait();wake.clear()
   while finals or latest is not None:
    if finals:
     job=finals.popleft()
     if latest is not None and latest[0]<=job[0]:latest=None
    else:job=latest;latest=None
    sid,audio,final=job
    text,dt=await asyncio.get_running_loop().run_in_executor(POOL,transcribe,audio,hotwords)
    await ws.send_json({'type':'final' if final else 'partial','segment':sid,'text':text,'audio_s':len(audio)/SR,'decode_s':dt,'elapsed_s':time.monotonic()-started})
   if stopping:
    await ws.send_json({'type':'done','elapsed_s':time.monotonic()-started});return
 def commit():
  nonlocal chunks,n,silence,since,speech_n,segment,latest
  if chunks and speech_n>=MIN_SPEECH:
   end=min(n,n-silence+TAIL_PAD)
   finals.append((segment,np.concatenate(chunks)[:end],True));segment+=1
   if len(finals)>8:raise ValueError('识别积压过多，请停止后重试。')
   latest=None;wake.set()
  chunks=[];n=0;silence=0;since=0;speech_n=0
 def process_frame(a):
  nonlocal chunks,n,silence,since,speech_n,pre,latest
  voiced=gate.probability(a)>=SPEECH_THRESHOLD
  if not chunks and not voiced:
   pre=np.concatenate([pre,a])[-4800:];return
  if not chunks:
   chunks=[pre];n=len(pre);pre=np.empty(0,np.float32)
  chunks.append(a);n+=len(a);since+=len(a)
  if voiced:speech_n+=len(a)
  silence=0 if voiced else silence+len(a)
  if silence>=int(SILENCE*SR) or n>=MAX_SEGMENT*SR:
   commit()
  elif voiced and speech_n>=MIN_SPEECH and n>=int(STEP*SR) and since>=int(STEP*SR):
   latest=(segment,np.concatenate(chunks),False);since=0;wake.set()
 task=asyncio.create_task(decode_loop())
 try:
  await ws.send_json({'type':'ready','sample_rate':SR,'hotword_count':len(hotwords)})
  while True:
   msg=await asyncio.wait_for(ws.receive(),timeout=120)
   if msg['type']=='websocket.disconnect':break
   if msg.get('text'):
    control=json.loads(msg['text'])
    if control.get('type')=='stop':
     if len(pending):process_frame(pending);pending=np.empty(0,np.float32)
     commit();stopping=True;wake.set();await task;break
    continue
   data=msg.get('bytes',b'')
   if len(data)>32000 or len(data)%2:raise ValueError('音频块格式错误。')
   a=np.frombuffer(data,dtype='<i2').astype(np.float32)/32768
   if not len(a):continue
   pending=np.concatenate([pending,a])
   while len(pending)>=512:
    process_frame(pending[:512]);pending=pending[512:]
   if time.monotonic()-started>3600:raise ValueError('已达到一小时会话上限，请重新开始。')
 except (WebSocketDisconnect,asyncio.TimeoutError):pass
 except Exception as e:
  try:await ws.send_json({'type':'error','message':str(e)})
  except Exception:pass
 finally:
  if not task.done():
   # Wait for current Metal inference before allowing a new session.
   latest=None;finals.clear();stopping=True;wake.set()
   try:await task
   except Exception:pass
  BUSY=False
  try:await ws.close()
  except Exception:pass
