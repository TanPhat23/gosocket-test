# Qualification canary listeners (run as root on the disposable test host). Every connection is logged
# to /run/canary.log; in the ruleset-ON phase the log must stay empty. Root is needed for port 80, and
# the log must live outside sticky /tmp (fs.protected_regular blocks root appending to others' files).
import socket, threading, time
LOG = "/run/canary.log"
def log(m):
    with open(LOG, "a") as f: f.write("%s %s\n" % (time.time(), m))
def tcp(ip, port):
    s = socket.socket(); s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1); s.bind((ip, port)); s.listen(50)
    while True:
        c, a = s.accept(); log("TCP %s:%d from %s" % (ip, port, a[0]))
        try: c.recv(1024); c.sendall(b"HTTP/1.0 200 OK\r\nContent-Length: 2\r\n\r\nok")
        except Exception: pass
        c.close()
def udp(ip, port):
    s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM); s.bind((ip, port))
    while True:
        s.recvfrom(1024); log("UDP %s:%d" % (ip, port))
for ip, p in [("0.0.0.0", 9999), ("0.0.0.0", 8080), ("0.0.0.0", 2222), ("198.51.100.50", 80), ("172.28.0.2", 5432)]:
    threading.Thread(target=tcp, args=(ip, p), daemon=True).start()
threading.Thread(target=udp, args=("198.51.100.50", 5353), daemon=True).start()
while True: time.sleep(3600)
