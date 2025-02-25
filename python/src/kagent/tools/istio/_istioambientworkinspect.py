import atexit
import logging
import os
import select
import socket
import threading
import time
from datetime import timedelta
from enum import Enum
from typing import Annotated, List, Optional

import grpc
from autogen_core.tools import FunctionTool
from google.protobuf import empty_pb2
from kubernetes import client, config
from kubernetes.stream import portforward

from .._utils import create_typed_fn_tool
from .thirdparty.protos.workload_telemetry_pb2_grpc import WorkloadEventsStub

logger = logging.getLogger(__name__)


class WorkloadClient:
    def __init__(self):
        self.pf = None
        self.channel = None
        self.stub = None

    def setup_port_forward(self, namespace, pod_name, remote_port=15006):
        # Load kube config
        config.load_kube_config()

        # Create API client
        v1 = client.CoreV1Api()

        logger.debug(f"Setting up port-forward to {pod_name}.{namespace}.{remote_port}")

        # Setup port forwarding
        # Note that this doesn't use `kubectl` but since it creates anonymous
        # UDS sockets, it's a bit tricky to coerce gRPC clients to use it.
        self.pf = portforward(v1.connect_get_namespaced_pod_portforward, pod_name, namespace, ports=str(remote_port))

        assert self.pf.connected, "Port forwarding connection failed"

        # Create a TCP server socket
        server = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        server.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
        server.bind(("localhost", 0))  # Bind to any available port
        server.listen(1)
        local_port = server.getsockname()[1]

        self.running = True

        # Function to handle the forwarding
        def forward_socket():
            try:
                while self.running:
                    # Set a timeout so we can check the running flag
                    server.settimeout(0.5)

                    try:
                        # Accept a connection from gRPC
                        client_sock, _ = server.accept()
                        logger.debug(f"Accepted connection on port {local_port}")

                        # Get the k8s socket
                        k8s_sock = self.pf.socket(remote_port)

                        # Set up two-way forwarding
                        while self.running:
                            try:
                                # Check which sockets are ready for reading
                                readable, _, _ = select.select([client_sock, k8s_sock], [], [], 0.5)

                                if client_sock in readable:
                                    data = client_sock.recv(4096)
                                    if not data:
                                        break  # Client socket closed
                                    k8s_sock.sendall(data)

                                if k8s_sock in readable:
                                    data = k8s_sock.recv(4096)
                                    if not data:
                                        break  # K8s socket closed
                                    client_sock.sendall(data)

                            except (ConnectionError, BrokenPipeError):
                                break

                        # Clean up this connection
                        client_sock.close()
                        # We don't close k8s_sock as it's managed by the port forward

                    except socket.timeout:
                        # Just a timeout on accept, continue
                        continue
                    except Exception as e:
                        if self.running:  # Only log if we're supposed to be running
                            logger.error(f"Socket error: {e}")
                        break

            finally:
                if not server._closed:
                    server.close()

        # Start the forwarding thread
        self.forward_thread = threading.Thread(target=forward_socket, daemon=True)
        self.forward_thread.start()

        # Save server socket for cleanup
        self.server = server

        # Create the gRPC channel
        self.channel = grpc.insecure_channel(f"localhost:{local_port}")
        self.stub = WorkloadEventsStub(self.channel)

        logger.debug(f"Port-forward established on localhost:{local_port}")
        return local_port

    def close(self):
        # Stop the forwarding thread
        if hasattr(self, "running"):
            self.running = False

        # Close the server socket
        if hasattr(self, "server") and self.server:
            self.server.close()

        # Close the gRPC channel
        if hasattr(self, "channel") and self.channel:
            self.channel.close()

        # Close the port forward
        if hasattr(self, "pf") and self.pf:
            self.pf.close()

        logger.debug("Port forwarding closed")

    def get_status(self):
        if not self.stub:
            raise RuntimeError("Client not initialized - call setup_port_forward first")
        return self.stub.Status(empty_pb2.Empty())

    def stream_events(self):
        if not self.stub:
            raise RuntimeError("Client not initialized - call setup_port_forward first")
        return self.stub.Events(empty_pb2.Empty())


def _run(namespace="", name="", duration_seconds=60):
    client = WorkloadClient()
    accumulated_events = []
    start_time = time.time()
    end_time = start_time + duration_seconds

    try:
        client.setup_port_forward(namespace, name)
        # Get initial status
        status = client.get_status()
        accumulated_events.append(f"Workload stats since enrollment: {status}")

        # Stream events for the specified duration
        for event_response in client.stream_events():
            # Check if time is up
            if time.time() >= end_time:
                break

            # Append the raw event response as a string
            accumulated_events.append(str(event_response))

    except Exception as e:
        accumulated_events.append(f"Error: {e}")
    finally:
        client.close()

    elapsed_time = time.time() - start_time
    actual_duration = min(elapsed_time, duration_seconds)
    accumulated_events.append(
        f"Monitoring completed: collected {len(accumulated_events) - 1} events over {timedelta(seconds=actual_duration)}"
    )

    return accumulated_events


async def _workload_inspect(
    pod_name: Annotated[Optional[str], "The name of the ambient-enrolled pod to collect L4 events from in JSON format"],
    ns: Annotated[Optional[str], "The namespace of the ambient-enrolled pod to collect L4 events from in JSON format"],
    inspect_duration: Annotated[
        Optional[float], "The duration to wait for ambient-enrolled pod L4 events to be collected"
    ],
) -> str:
    return _run(ns if ns else "default", pod_name, inspect_duration)


workload_inspect = FunctionTool(
    _workload_inspect,
    description="Collect live L4 connection events from a single ambient-enrolled Kubernetes pod for some duration, in a Solo mesh",
    name="workload_inspect",
)

WorkloadInspect, WorkloadInspectConfig = create_typed_fn_tool(
    workload_inspect, "kagent.tools.istio.WorkloadInspect", "WorkloadInspect"
)
