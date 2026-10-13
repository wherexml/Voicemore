"""Stateful 16 kHz Silero ONNX detector; CPU only, one state per session.
Tensor contract follows pinned upstream utils_vad.py (see vad/manifest.json).
"""
from pathlib import Path
import hashlib,json
import numpy as np
import onnxruntime as ort
ROOT=Path(__file__).parent

def load_detector():
 p=ROOT/'vad/silero_vad.onnx'
 manifest=json.loads((ROOT/'vad/manifest.json').read_text())
 if hashlib.sha256(p.read_bytes()).hexdigest()!=manifest['sha256']:
  raise RuntimeError('VAD model checksum mismatch')
 opts=ort.SessionOptions();opts.inter_op_num_threads=1;opts.intra_op_num_threads=1
 return ort.InferenceSession(str(p),sess_options=opts,providers=['CPUExecutionProvider'])

class SpeechGate:
 def __init__(self,session):
  self.session=session
  self.state=np.zeros((2,1,128),dtype=np.float32)
  self.context=np.zeros((1,64),dtype=np.float32)
 def probability(self,frame):
  if len(frame)>512:raise ValueError('VAD expects at most 512 samples')
  x=np.pad(frame,(0,512-len(frame))).astype(np.float32).reshape(1,512)
  x=np.concatenate([self.context,x],axis=1)
  out,self.state=self.session.run(None,{'input':x,'state':self.state,'sr':np.array(16000,dtype=np.int64)})
  self.context=x[:,-64:].copy()
  return float(out[0,0])
