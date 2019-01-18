import os
import re
import socket
import tempfile
import time
import urllib
from contextlib import contextmanager
from functools import partial as p

import docker
import requests
import semver
import yaml
from kubernetes import config as kube_config

import tests.helpers.kubernetes.utils as k8s
from tests.helpers.assertions import container_cmd_exit_0, tcp_socket_open
from tests.helpers.formatting import print_dp_or_event
from tests.helpers.kubernetes.agent import Agent
from tests.helpers.util import (
    container_ip,
    fake_backend,
    get_docker_client,
    get_host_ip,
    retry,
    wait_for,
    TEST_SERVICES_DIR,
)
from tests.packaging.common import get_container_file_content

MINIKUBE_VERSION = os.environ.get("MINIKUBE_VERSION")
MINIKUBE_LOCALKUBE_VERSION = "v0.28.2"
MINIKUBE_KUBEADM_VERSION = "v0.33.1"
MINIKUBE_IMAGE_TIMEOUT = int(os.environ.get("MINIKUBE_IMAGE_TIMEOUT", 300))
K8S_API_PORT = 8443
K8S_RELEASE_URL = "https://storage.googleapis.com/kubernetes-release/release/stable.txt"
K8S_MIN_VERSION = "1.7.0"
K8S_MIN_KUBEADM_VERSION = "1.11.0"


def container_is_running(client, name):
    try:
        cont = client.containers.get(name)
        cont.reload()
        if cont.status.lower() != "running":
            return False
        return container_ip(cont)
    except docker.errors.NotFound:
        return False
    except docker.errors.APIError as e:
        if "is not running" in str(e):
            return False
        raise


def get_free_port():
    sock = socket.socket()
    sock.bind(("", 0))
    return sock.getsockname()[1]


def get_latest_k8s_version():
    version = None
    with urllib.request.urlopen(K8S_RELEASE_URL) as resp:
        version = resp.read().decode("utf-8").strip()
    assert version, "Failed to get latest K8S version from %s" % K8S_RELEASE_URL
    return version.lstrip("v")


def has_docker_image(client, name, tag=None):
    try:
        if tag:
            client.images.get(name + ":" + tag)
        else:
            client.images.get(name)
        return True
    except docker.errors.ImageNotFound:
        return False


class Minikube:  # pylint: disable=too-many-instance-attributes
    def __init__(self):
        self.agent = Agent()
        self.bootstrapper = None
        self.client = None
        self.cluster_name = None
        self.container = None
        self.container_ip = None
        self.host_client = get_docker_client()
        self.k8s_version = None
        self.kubeconfig = None
        self.name = None
        self.registry_port = 5000
        self.version = None
        self.yamls = []

    def get_k8s_version(self, k8s_version):
        if k8s_version:
            k8s_latest_version = retry(get_latest_k8s_version, urllib.error.URLError)
            if k8s_version.lower() == "latest":
                k8s_version = k8s_latest_version
            k8s_version = k8s_version.lstrip("v")
            assert re.match(r"^\d+\.\d+\.\d+$", k8s_version), "Invalid K8S version '%s'" % k8s_version
            assert semver.match(k8s_version, ">=" + K8S_MIN_VERSION), "K8S version %s not supported" % k8s_version
            assert semver.match(k8s_version, "<=" + k8s_latest_version), "K8S version %s not supported" % k8s_version
            self.k8s_version = "v" + k8s_version
        return self.k8s_version

    def get_version(self):
        if MINIKUBE_VERSION:
            self.version = MINIKUBE_VERSION
        elif self.k8s_version:
            if semver.match(self.k8s_version.lstrip("v"), ">=" + K8S_MIN_KUBEADM_VERSION):
                self.version = MINIKUBE_KUBEADM_VERSION
            else:
                self.version = MINIKUBE_LOCALKUBE_VERSION
        return self.version

    def get_client(self):
        if self.container:
            self.container.reload()
            self.container_ip = container_ip(self.container)
            assert wait_for(
                p(tcp_socket_open, self.container_ip, 2375)
            ), "timed out waiting for docker engine in minikube!"
            self.client = docker.DockerClient(base_url="tcp://%s:2375" % self.container_ip, version="auto")
        return self.client

    def load_kubeconfig(self, kubeconfig_path="/kubeconfig", timeout=300):
        def kubeconfig_exists():
            try:
                return container_cmd_exit_0(self.container, "test -f %s" % kubeconfig_path)
            except requests.exceptions.RequestException:
                return False

        assert wait_for(kubeconfig_exists, timeout_seconds=timeout, interval_seconds=2), (
            "timed out waiting for the minikube cluster to be ready!\n\n%s\n\n" % self.get_logs()
        )
        time.sleep(2)
        content = get_container_file_content(self.container, kubeconfig_path)
        self.kubeconfig = yaml.load(content)
        current_context = self.kubeconfig.get("current-context")
        for context in self.kubeconfig.get("contexts"):
            if context.get("name") == current_context:
                self.cluster_name = context.get("context").get("cluster")
                break
        assert self.cluster_name, "cluster not found in %s:\n%s" % (kubeconfig_path, content)
        with tempfile.NamedTemporaryFile(mode="w") as fd:
            fd.write(content)
            fd.flush()
            kube_config.load_kube_config(config_file=fd.name)

    def get_bootstrapper(self):
        if self.container:
            if container_cmd_exit_0(self.container, "which localkube"):
                return "localkube"
            if container_cmd_exit_0(self.container, "which kubeadm"):
                return "kubeadm"
        return None

    def connect(self, name, timeout, k8s_version=None):
        self.name = name
        if self.get_k8s_version(k8s_version) and self.get_version():
            assert wait_for(
                p(has_docker_image, self.host_client, "minikube", self.version),
                timeout_seconds=MINIKUBE_IMAGE_TIMEOUT,
                interval_seconds=2,
            ), ("timed out waiting for minikube:%s image!" % self.version)
        print("\nConnecting to %s container ..." % self.name)
        assert wait_for(
            p(container_is_running, self.host_client, self.name), timeout_seconds=timeout, interval_seconds=2
        ), ("timed out waiting for container %s!" % self.name)
        self.container = self.host_client.containers.get(self.name)
        self.load_kubeconfig(timeout=timeout)
        self.client = self.get_client()
        assert wait_for(
            p(tcp_socket_open, self.container_ip, K8S_API_PORT)
        ), "timed out waiting for k8s api in minikube!"
        self.get_bootstrapper()

    def deploy(self, k8s_version, timeout, options=None):
        assert self.get_k8s_version(k8s_version), "K8S version not defined"
        assert self.get_version(), "Failed to get minikube version"
        if options is None:
            options = {}
        if tcp_socket_open("127.0.0.1", self.registry_port):
            self.registry_port = get_free_port()
        if container_is_running(self.host_client, "minikube"):
            self.host_client.containers.get("minikube").remove(force=True, v=True)
        if not options:
            options = {
                "name": "minikube",
                "privileged": True,
                "environment": {"K8S_VERSION": self.k8s_version, "TIMEOUT": str(timeout)},
                "ports": {"%d/tcp" % self.registry_port: self.registry_port},
            }
        if semver.match(self.version.lstrip("v"), ">" + MINIKUBE_LOCALKUBE_VERSION.lstrip("v")):
            options["command"] = "/lib/systemd/systemd"
        else:
            options["command"] = "sleep inf"
        print("\nBuilding minikube:%s image ..." % self.version)
        build_opts = dict(buildargs={"MINIKUBE_VERSION": self.version}, tag="minikube:%s" % self.version)
        image, _ = self.build_image("minikube", build_opts, self.host_client)
        print("\nDeploying minikube %s cluster ..." % self.k8s_version)
        self.container = self.host_client.containers.run(image.id, detach=True, **options)
        self.name = self.container.name
        self.container.exec_run("start-minikube.sh", detach=True)
        self.load_kubeconfig(timeout=timeout)
        self.client = self.get_client()
        assert wait_for(
            p(tcp_socket_open, self.container_ip, K8S_API_PORT)
        ), "timed out waiting for k8s api in minikube!"
        self.get_bootstrapper()

    def start_registry(self):
        if not self.client:
            self.client = self.get_client()
        print("\nStarting registry container localhost:%d in minikube ..." % self.registry_port)
        retry(
            p(
                self.client.containers.run,
                image="registry:2.7",
                name="registry",
                detach=True,
                environment={"REGISTRY_HTTP_ADDR": "0.0.0.0:%d" % self.registry_port},
                ports={"%d/tcp" % self.registry_port: self.registry_port},
            ),
            docker.errors.DockerException,
        )
        assert wait_for(
            p(tcp_socket_open, self.container_ip, self.registry_port)
        ), "timed out waiting for registry to start!"

    def build_image(self, dockerfile_dir, build_opts=None, client=None):
        if os.path.isdir(os.path.join(TEST_SERVICES_DIR, dockerfile_dir)):
            dockerfile_dir = os.path.join(TEST_SERVICES_DIR, dockerfile_dir)
        assert os.path.isdir(dockerfile_dir), "Dockerfile directory %s not found!" % dockerfile_dir
        if build_opts is None:
            build_opts = {}
        if client is None:
            client = self.get_client()
        print("\nBuilding image from %s ..." % dockerfile_dir)
        return retry(
            p(client.images.build, path=dockerfile_dir, rm=True, forcerm=True, **build_opts), docker.errors.BuildError
        )

    def exec_kubectl(self, command, namespace=None):
        if self.container:
            command = "kubectl %s" % command
            if namespace:
                command += " -n %s" % namespace
            else:
                command += " --all-namespaces"
            return self.container.exec_run(command).output.decode("utf-8")
        return ""

    @contextmanager
    def deploy_k8s_yamls(self, yamls=None, namespace=None, timeout=180):
        if yamls is None:
            yamls = []
        self.yamls = []
        for yaml_file in yamls:
            assert os.path.isfile(yaml_file), '"%s" not found!' % yaml_file
            with open(yaml_file, "r") as fd:
                for doc in yaml.load_all(fd.read()):
                    kind = doc["kind"]
                    name = doc["metadata"]["name"]
                    api_client = k8s.api_client_from_version(doc["apiVersion"])
                    if not doc.get("metadata", {}).get("namespace"):
                        if "metadata" not in doc:
                            doc["metadata"] = {}
                        doc["metadata"]["namespace"] = namespace
                    if k8s.has_resource(name, kind, api_client, namespace):
                        print('Deleting %s "%s" ...' % (kind, name))
                        k8s.delete_resource(name, kind, api_client, namespace=namespace)
                    print("Creating %s from %s ..." % (kind, yaml_file))
                    k8s.create_resource(doc, api_client, namespace=namespace, timeout=timeout)
                    self.yamls.append(doc)

        for doc in filter(lambda d: d["kind"] == "Deployment", self.yamls):
            name = doc["metadata"]["name"]
            print("Waiting for deployment %s to be ready ..." % name)
            try:
                start_time = time.time()
                k8s.wait_for_deployment(doc, timeout)
                print("Waited %d seconds" % (time.time() - start_time))
            except AssertionError:
                print(self.exec_kubectl("describe deployment %s" % name, namespace))
                for pod in k8s.get_all_pods(namespace):
                    print(self.exec_kubectl("describe pod %s" % pod.metadata.name, namespace))
                raise

        try:
            yield
        finally:
            for doc in self.yamls:
                kind = doc["kind"]
                name = doc["metadata"]["name"]
                api_client = k8s.api_client_from_version(doc["apiVersion"])
                print('Deleting %s "%s" ...' % (kind, name))
                k8s.delete_resource(name, kind, api_client, namespace=namespace)
            self.yamls = []

    def pull_agent_image(self, name, tag, image_id=None):
        if image_id and has_docker_image(self.client, image_id):
            return self.client.images.get(image_id)

        if has_docker_image(self.client, name, tag):
            return self.client.images.get(name + ":" + tag)

        return self.client.images.pull(name, tag=tag)

    @contextmanager
    def run_agent(self, agent_image, yamls=None, yamls_timeout=180, **kwargs):
        namespace = kwargs.get("namespace", "default")
        kwargs["image_name"] = agent_image["name"]
        kwargs["image_tag"] = agent_image["tag"]
        with self.deploy_k8s_yamls(yamls, namespace=namespace, timeout=yamls_timeout):
            with fake_backend.start(ip_addr=get_host_ip()) as backend:
                with self.agent.deploy(backend=backend, cluster_name=self.cluster_name, **kwargs):
                    try:
                        yield self.agent, backend
                    finally:
                        print("\nAgent status:\n%s" % self.agent.get_status())
                        print("\nAgent logs:\n%s" % self.agent.get_logs())
                        if backend.datapoints:
                            print("\nDatapoints received:")
                            for dp in backend.datapoints:
                                print_dp_or_event(dp)
                        if backend.events:
                            print("\nEvents received:")
                            for event in backend.events:
                                print_dp_or_event(event)
                        self.agent.delete()
                        self.agent = Agent()

    def get_logs(self):
        if self.container:
            self.container.reload()
            if self.container.status.lower() != "running":
                return "%s container is not running" % self.name
            return "/var/log/start-minikube.log:\n%s" % get_container_file_content(
                self.container, "/var/log/start-minikube.log"
            )
        return ""
