import os
import time
import socket
from locust import User, task, between


class RedisUser(User):
    wait_time = between(0.0, 0.005)
    redis_host = None
    redis_port = None

    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        if not (RedisUser.redis_host and RedisUser.redis_port):
            if self.host and self.host.startswith("tcp://"):
                host_port = self.host[6:].split(":")
                RedisUser.redis_host = host_port[0]
                RedisUser.redis_port = int(host_port[1]) if len(host_port) > 1 else 7171
            else:
                RedisUser.redis_host = os.getenv("REDIS_HOST", "localhost")
                RedisUser.redis_port = int(os.getenv("REDIS_PORT", "7171"))

    def on_start(self):
        self.client = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        self.client.setsockopt(socket.IPPROTO_TCP, socket.TCP_NODELAY, 1)
        self.client.connect((RedisUser.redis_host, RedisUser.redis_port))

    def on_stop(self):
        self.client.close()

    def send_command(self, command):
        try:
            self.client.sendall(f"{command}\n".encode())
            response = self.client.recv(4096)
            return response.decode()
        except Exception as e:
            return f"ERROR: {e}"

    @task
    def put_task(self):
        start_time = time.perf_counter()
        key = f"key_{int(time.perf_counter() * 1000)}"
        value = "test_value"
        command = f"PUT {key} {value}"
        response = self.send_command(command)
        self.environment.events.request.fire(
            request_type="PUT",
            name="redis_put",
            response_time=(time.perf_counter() - start_time) * 1000,
            response_length=len(response),
            exception=None if "OK" in response else Exception("PUT command failed"),
        )

    @task
    def get_task(self):
        start_time = time.perf_counter()
        key = f"key_{int(time.perf_counter() * 1000) - 1}"
        command = f"GET {key}"
        response = self.send_command(command)
        self.environment.events.request.fire(
            request_type="GET",
            name="redis_get",
            response_time=(time.perf_counter() - start_time) * 1000,
            response_length=len(response),
            exception=None if response else Exception("Key not found"),
        )