"""Private Voice Input worker. Executed with an app-owned isolated Python."""
import os, sys, threading, time
from pathlib import Path
# -I excludes the script directory; add only this app-owned worker directory.
sys.path.insert(0, str(Path(__file__).resolve().parent))
parent = int(os.environ['VOICE_INPUT_PARENT'])
def watch_parent():
    while True:
        time.sleep(2)
        if os.getppid() != parent:
            os._exit(0)
threading.Thread(target=watch_parent, daemon=True).start()
import uvicorn
if __name__ == '__main__':
    uvicorn.run('server:app', host='127.0.0.1', port=int(os.environ['VOICE_INPUT_PORT']), ws_max_size=65536, log_level='warning')
