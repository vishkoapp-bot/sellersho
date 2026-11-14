"""Bounded local UI-only raw TLS relay; never a general forwarding service."""
import json
import re
import socket
import socketserver
import subprocess
import threading
import time

def validate_target(target):
    if set(target)!={'nonce','network','network_id','tls_cid','relay_cid'} or not re.fullmatch('[a-f0-9]{24}',target.get('nonce','')):
        raise ValueError('Invalid private relay capability')
    if target['network']!='sellersho-cap-'+target['nonce'] or any(not re.fullmatch('[a-f0-9]{64}',target[k]) for k in ('network_id','tls_cid','relay_cid')):
        raise ValueError('Invalid disposable relay identity')

class BrowserRelay:
    def __init__(self,target,verify):
        validate_target(target)
        self.target=target;self.verify=verify;self.lock=threading.Lock();self.active={};self.closed=False
        self.counts={'connections':0,'refused':0,'bytes_to_tls':0,'bytes_from_tls':0,'errors':0}
        self.slots=threading.BoundedSemaphore(32);self.deadline=time.monotonic()+240
        owner=self
        class Handler(socketserver.BaseRequestHandler):
            def handle(self):owner.forward(self.request)
        class Server(socketserver.ThreadingTCPServer):
            daemon_threads=True
            allow_reuse_address=False
        self.verify()
        self.server=Server(('127.0.0.1',0),Handler)
        self.port=self.server.server_address[1]
        self.thread=threading.Thread(target=self.server.serve_forever,daemon=True)
        self.thread.start()
        self.timer=threading.Timer(240,self.close);self.timer.daemon=True;self.timer.start()
    def forward(self,client):
        if self.closed or time.monotonic()>self.deadline or not self.slots.acquire(blocking=False):
            with self.lock:self.counts['refused']+=1
            return
        proc=None;reader=None;timer=None
        try:
            # The caller uses the same exact Docker identity guard as provisioning.
            self.verify()
            pump="const s=require('node:net').connect(443,'tls');s.setTimeout(120000,()=>s.destroy());s.on('error',()=>process.exit(1));s.on('close',()=>process.exit(0));process.stdin.pipe(s);s.pipe(process.stdout);"
            with self.lock:
                if self.closed:return
                proc=subprocess.Popen(['docker','exec','-i',self.target['relay_cid'],'node','-e',pump],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL)
                self.active[proc]=client;self.counts['connections']+=1
            client.settimeout(120)
            def kill_connection():
                try:client.shutdown(socket.SHUT_RDWR)
                except OSError:pass
                if proc.poll() is None:proc.terminate()
            timer=threading.Timer(120,kill_connection);timer.daemon=True;timer.start()
            def response():
                total=0
                try:
                    while chunk:=proc.stdout.read1(65536):
                        total+=len(chunk)
                        if total>64*1024*1024:break
                        client.sendall(chunk)
                        with self.lock:self.counts['bytes_from_tls']+=len(chunk)
                except (OSError,ValueError):pass
                finally:kill_connection()
            reader=threading.Thread(target=response,daemon=True);reader.start();total=0
            while chunk:=client.recv(65536):
                total+=len(chunk)
                if total>64*1024*1024:raise ValueError('Relay byte limit')
                proc.stdin.write(chunk);proc.stdin.flush()
                with self.lock:self.counts['bytes_to_tls']+=len(chunk)
        except Exception:
            with self.lock:self.counts['errors']+=1
        finally:
            if timer:timer.cancel()
            if proc:
                if proc.poll() is None:proc.terminate()
                try:proc.wait(timeout=5)
                except subprocess.TimeoutExpired:proc.kill();proc.wait(timeout=5)
                if reader:reader.join(timeout=5)
                proc.stdin.close();proc.stdout.close()
                with self.lock:self.active.pop(proc,None)
            self.slots.release()
    def close(self):
        with self.lock:
            if self.closed:return
            self.closed=True;active=list(self.active.items())
        self.server.shutdown();self.server.server_close();self.timer.cancel()
        for proc,client in active:
            try:client.shutdown(socket.SHUT_RDWR)
            except OSError:pass
            if proc.poll() is None:proc.terminate()
        deadline=time.monotonic()+10
        while self.active and time.monotonic()<deadline:time.sleep(.05)
        if self.active:raise RuntimeError('Relay children failed to drain')
