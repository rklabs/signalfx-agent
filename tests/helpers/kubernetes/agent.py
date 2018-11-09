import os
from contextlib import contextmanager

import yaml
import tests.helpers.kubernetes.utils as k8s
from tests.helpers.util import ensure_always, get_internal_status_host

CUR_DIR = os.path.dirname(os.path.realpath(__file__))
AGENT_YAMLS_DIR = os.environ.get("AGENT_YAMLS_DIR", os.path.realpath(os.path.join(CUR_DIR, "../../../deployments/k8s")))
AGENT_CONFIGMAP_PATH = os.environ.get("AGENT_CONFIGMAP_PATH", os.path.join(AGENT_YAMLS_DIR, "configmap.yaml"))
AGENT_DAEMONSET_PATH = os.environ.get("AGENT_DAEMONSET_PATH", os.path.join(AGENT_YAMLS_DIR, "daemonset.yaml"))
AGENT_SERVICEACCOUNT_PATH = os.environ.get(
    "AGENT_SERVICEACCOUNT_PATH", os.path.join(AGENT_YAMLS_DIR, "serviceaccount.yaml")
)
AGENT_CLUSTERROLE_PATH = os.environ.get("AGENT_CLUSTERROLE_PATH", os.path.join(AGENT_YAMLS_DIR, "clusterrole.yaml"))
AGENT_CLUSTERROLEBINDING_PATH = os.environ.get(
    "AGENT_CLUSTERROLEBINDING_PATH", os.path.join(AGENT_YAMLS_DIR, "clusterrolebinding.yaml")
)
AGENT_STATUS_COMMAND = ["/bin/sh", "-c", "agent-status"]


class Agent:  # pylint: disable=too-many-instance-attributes
    def __init__(self):
        self.agent_yaml = None
        self.backend = None
        self.cluster_name = None
        self.clusterrole_name = None
        self.clusterrole_yaml = None
        self.clusterrolebinding_name = None
        self.clusterrolebinding_yaml = None
        self.configmap_name = None
        self.configmap_yaml = None
        self.daemonset_labels = None
        self.daemonset_name = None
        self.daemonset_yaml = None
        self.image_name = None
        self.image_tag = None
        self.monitors = []
        self.observer = None
        self.namespace = None
        self.pods = []
        self.serviceaccount_name = None
        self.serviceaccount_yaml = None

    def create_agent_secret(self, secret="testing123"):
        if not k8s.has_secret("signalfx-agent", namespace=self.namespace):
            print('Creating secret "signalfx-agent" ...')
            k8s.create_secret("signalfx-agent", "access-token", secret, namespace=self.namespace)

    def create_agent_serviceaccount(self, serviceaccount_path):
        self.serviceaccount_yaml = yaml.load(open(serviceaccount_path).read())
        self.serviceaccount_name = self.serviceaccount_yaml["metadata"]["name"]
        if not k8s.has_serviceaccount(self.serviceaccount_name, namespace=self.namespace):
            print('Creating service account "%s" from %s ...' % (self.serviceaccount_name, serviceaccount_path))
            k8s.create_serviceaccount(body=self.serviceaccount_yaml, namespace=self.namespace)

    def create_agent_clusterrole(self, clusterrole_path, clusterrolebinding_path):
        self.clusterrole_yaml = yaml.load(open(clusterrole_path).read())
        self.clusterrole_name = self.clusterrole_yaml["metadata"]["name"]
        self.clusterrolebinding_yaml = yaml.load(open(clusterrolebinding_path).read())
        self.clusterrolebinding_name = self.clusterrolebinding_yaml["metadata"]["name"]
        if self.namespace != "default":
            self.clusterrole_name = self.clusterrole_name + "-" + self.namespace
            self.clusterrole_yaml["metadata"]["name"] = self.clusterrole_name
            self.clusterrolebinding_name = self.clusterrolebinding_name + "-" + self.namespace
            self.clusterrolebinding_yaml["metadata"]["name"] = self.clusterrolebinding_name
        if self.clusterrolebinding_yaml["roleRef"]["kind"] == "ClusterRole":
            self.clusterrolebinding_yaml["roleRef"]["name"] = self.clusterrole_name
        for subject in self.clusterrolebinding_yaml["subjects"]:
            subject["namespace"] = self.namespace
        if not k8s.has_clusterrole(self.clusterrole_name):
            print('Creating cluster role "%s" from %s ...' % (self.clusterrole_name, clusterrole_path))
            k8s.create_clusterrole(self.clusterrole_yaml)
        if not k8s.has_clusterrolebinding(self.clusterrolebinding_name):
            print(
                'Creating cluster role binding "%s" from %s ...'
                % (self.clusterrolebinding_name, clusterrolebinding_path)
            )
            k8s.create_clusterrolebinding(self.clusterrolebinding_yaml)

    def create_agent_configmap(self, configmap_path):
        self.configmap_yaml = yaml.load(open(configmap_path).read())
        self.configmap_name = self.configmap_yaml["metadata"]["name"]
        self.delete_agent_configmap()
        self.agent_yaml = yaml.load(self.configmap_yaml["data"]["agent.yaml"])
        del self.agent_yaml["observers"]
        if not self.observer and self.agent_yaml.get("observers"):
            del self.agent_yaml["observers"]
        elif self.observer == "k8s-api":
            self.agent_yaml["observers"] = [
                {"type": self.observer, "kubernetesAPI": {"authType": "serviceAccount", "skipVerify": False}}
            ]
        elif self.observer == "k8s-kubelet":
            self.agent_yaml["observers"] = [
                {"type": self.observer, "kubeletAPI": {"authType": "serviceAccount", "skipVerify": True}}
            ]
        elif self.observer == "docker":
            self.agent_yaml["observers"] = [{"type": self.observer, "dockerURL": "unix:///var/run/docker.sock"}]
        else:
            self.agent_yaml["observers"] = [{"type": self.observer}]
        self.agent_yaml["globalDimensions"]["kubernetes_cluster"] = self.cluster_name
        self.agent_yaml["intervalSeconds"] = 5
        self.agent_yaml["sendMachineID"] = True
        self.agent_yaml["useFullyQualifiedHost"] = False
        self.agent_yaml["internalStatusHost"] = get_internal_status_host()
        if self.backend:
            self.agent_yaml["ingestUrl"] = "http://%s:%d" % (self.backend.ingest_host, self.backend.ingest_port)
            self.agent_yaml["apiUrl"] = "http://%s:%d" % (self.backend.api_host, self.backend.api_port)
        if self.agent_yaml.get("metricsToExclude"):
            del self.agent_yaml["metricsToExclude"]
        del self.agent_yaml["monitors"]
        self.agent_yaml["monitors"] = self.monitors
        self.configmap_yaml["data"]["agent.yaml"] = yaml.dump(self.agent_yaml)
        print(
            "Creating configmap for observer=%s and monitor(s)=%s from %s ..."
            % (self.observer, ",".join([m["type"] for m in self.monitors]), configmap_path)
        )
        k8s.create_configmap(body=self.configmap_yaml, namespace=self.namespace)

    def create_agent_daemonset(self, daemonset_path):
        self.daemonset_yaml = yaml.load(open(daemonset_path).read())
        self.daemonset_name = self.daemonset_yaml["metadata"]["name"]
        self.daemonset_labels = self.daemonset_yaml["spec"]["selector"]["matchLabels"]
        self.delete_agent_daemonset()
        self.daemonset_yaml["spec"]["template"]["spec"]["containers"][0]["resources"] = {"requests": {"cpu": "100m"}}
        if self.image_name and self.image_tag:
            print(
                'Creating daemonset "%s" for %s:%s from %s ...'
                % (self.daemonset_name, self.image_name, self.image_tag, daemonset_path)
            )
            self.daemonset_yaml["spec"]["template"]["spec"]["containers"][0]["image"] = (
                self.image_name + ":" + self.image_tag
            )
        else:
            print('Creating daemonset "%s" from %s ...' % (self.daemonset_name, daemonset_path))
        k8s.create_daemonset(body=self.daemonset_yaml, namespace=self.namespace)
        assert ensure_always(lambda: k8s.daemonset_is_ready(self.daemonset_name, namespace=self.namespace), 5)
        str_labels = ",".join(["%s=%s" % keyval for keyval in self.daemonset_labels.items()])
        self.pods = k8s.get_pods_by_labels(str_labels, namespace=self.namespace)
        assert self.pods, "no agent pods found"
        assert all([k8s.pod_is_ready(pod.metadata.name, namespace=self.namespace) for pod in self.pods])

    @contextmanager
    def deploy(self, **kwargs):
        self.observer = kwargs.get("observer")
        self.monitors = kwargs.get("monitors")
        self.cluster_name = kwargs.get("cluster_name", "minikube")
        self.backend = kwargs.get("backend")
        self.image_name = kwargs.get("image_name")
        self.image_tag = kwargs.get("image_tag")
        self.namespace = kwargs.get("namespace", "default")

        self.create_agent_secret()
        self.create_agent_serviceaccount(kwargs.get("serviceaccount_path", AGENT_SERVICEACCOUNT_PATH))
        self.create_agent_clusterrole(
            kwargs.get("clusterrole_path", AGENT_CLUSTERROLE_PATH),
            kwargs.get("clusterrolebinding_path", AGENT_CLUSTERROLEBINDING_PATH),
        )
        self.create_agent_configmap(kwargs.get("configmap_path", AGENT_CONFIGMAP_PATH))
        self.create_agent_daemonset(kwargs.get("daemonset_path", AGENT_DAEMONSET_PATH))

        yield self

    def delete_agent_daemonset(self):
        if self.daemonset_name and k8s.has_daemonset(self.daemonset_name, namespace=self.namespace):
            print('Deleting daemonset "%s" ...' % self.daemonset_name)
            k8s.delete_daemonset(self.daemonset_name, namespace=self.namespace)

    def delete_agent_configmap(self):
        if self.configmap_name and k8s.has_configmap(self.configmap_name, namespace=self.namespace):
            print('Deleting configmap "%s" ...' % self.configmap_name)
            k8s.delete_configmap(self.configmap_name, namespace=self.namespace)

    def delete(self):
        self.delete_agent_daemonset()
        self.delete_agent_configmap()

    def get_status(self, command=None):
        if not command:
            command = AGENT_STATUS_COMMAND
        output = ""
        for pod in self.pods:
            output += "pod/%s:\n" % pod.metadata.name
            output += k8s.exec_pod_command(pod.metadata.name, command, namespace=self.namespace) + "\n"
        return output.strip()

    def get_logs(self):
        output = ""
        for pod in self.pods:
            output += "pod/%s\n" % pod.metadata.name
            output += k8s.get_pod_logs(pod.metadata.name, namespace=self.namespace) + "\n"
        return output.strip()
