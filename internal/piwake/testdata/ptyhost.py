"""Runs a command in a pseudo terminal and lets a test drive it.

Usage: ptyhost.py <cwd> <command> [args...]. The environment is inherited.
Reads one JSON request per line on stdin and answers each with one JSON line:
  {"op":"send","data":"text"}   type text into the terminal
  {"op":"tail","n":4000}        -> {"text": the last n bytes the command wrote}
  {"op":"pid"}                  -> {"pid": the command's pid}
  {"op":"alive"}                -> {"alive": bool}
The command is killed, with its process group, when stdin closes.
"""
import fcntl, json, os, pty, signal, struct, sys, termios, threading

cwd, argv = sys.argv[1], sys.argv[2:]
pid, fd = pty.fork()
if pid == 0:
    fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 120, 0, 0))
    os.chdir(cwd)
    os.execvp(argv[0], argv)

buf, lock, done = bytearray(), threading.Lock(), threading.Event()

def pump():
    while True:
        try:
            data = os.read(fd, 65536)
        except OSError:
            break
        if not data:
            break
        with lock:
            buf.extend(data)
    try:
        os.waitpid(pid, 0)
    except ChildProcessError:
        pass
    done.set()

threading.Thread(target=pump, daemon=True).start()

try:
    for line in sys.stdin:
        req = json.loads(line)
        op = req["op"]
        if op == "send":
            os.write(fd, req["data"].encode())
            resp = {}
        elif op == "tail":
            with lock:
                resp = {"text": bytes(buf[-req.get("n", 4000):]).decode("utf-8", "replace")}
        elif op == "pid":
            resp = {"pid": pid}
        elif op == "alive":
            resp = {"alive": not done.is_set()}
        else:
            resp = {"error": "unknown op"}
        print(json.dumps(resp), flush=True)
finally:
    try:
        os.killpg(pid, signal.SIGKILL)
    except OSError:
        pass
