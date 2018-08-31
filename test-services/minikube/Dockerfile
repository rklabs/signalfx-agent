# Portions Copyright 2016 The Kubernetes Authors All rights reserved.
# Portions Copyright 2018 AspenMesh
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.
#
# Based on:
# https://github.com/kubernetes/minikube/tree/master/deploy/docker/localkube-dind

FROM ubuntu:16.04
ARG MINIKUBE_VERSION=latest

# Install minikube dependencies
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update -y && \
    apt-get -yy -q --no-install-recommends install \
    apt-transport-https \
    aufs-tools \
    bridge-utils \
    ca-certificates \
    cifs-utils \
    conntrack \
    curl \
    ebtables \
    ethtool \
    glusterfs-client \
    gnupg2 \
    ipcalc \
    iptables \
    nfs-common \
    netcat \
    socat \
    software-properties-common \
    gcc \
    sudo \
    linux-image-$(uname -r) \
    iproute \
    && apt-get clean && \
    rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

# Install docker
# RUN curl -fsSL get.docker.com -o /tmp/get-docker.sh && \
#     sh /tmp/get-docker.sh && \
#     rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*

#RUN cd /tmp && \
#    curl -sSl -O https://download.docker.com/linux/static/stable/x86_64/docker-17.03.1-ce.tgz && \
#    tar -xf docker*.tgz && \
#    chmod a+x docker/* && \
#    chown root:root docker/* && \
#    cp docker/* /usr/bin/
#RUN apt-get update && \
RUN curl -fsSL https://download.docker.com/linux/$(. /etc/os-release; echo "$ID")/gpg > /tmp/dkey; apt-key add /tmp/dkey && \
    add-apt-repository \
       "deb [arch=amd64] https://download.docker.com/linux/$(. /etc/os-release; echo "$ID") \
       $(lsb_release -cs) \
       stable" && \
    apt-get update && \
    apt-get install -y docker-ce=17.03.3~ce-0~ubuntu-xenial
#    apt-get update && \
VOLUME /var/lib/docker
EXPOSE 2375

# Install minikube
RUN curl -sSl -o /usr/local/bin/minikube https://storage.googleapis.com/minikube/releases/${MINIKUBE_VERSION}/minikube-linux-amd64
RUN chmod a+x /usr/local/bin/minikube
ENV MINIKUBE_WANTUPDATENOTIFICATION=false
ENV MINIKUBE_WANTREPORTERRORPROMPT=false
ENV CHANGE_MINIKUBE_NONE_USER=true
EXPOSE 8443

# minikube --vm-driver=none checks systemctl before starting.  Instead of
# setting up a real systemd environment, install this shim to tell minikube
# what it wants to know: localkube isn't started yet.
#COPY fake-systemctl.sh /usr/local/bin/systemctl
#RUN chmod a+x /usr/local/bin/systemctl
#ENV XDG_RUNTIME_DIR=/run/user/`id -u`
#RUN systemctl restart systemd-logind.service
ENV SHELL /bin/bash
RUN curl https://raw.githubusercontent.com/daleobrien/start-stop-daemon/master/start-stop-daemon.c > start-stop-daemon.c \
    &&  gcc start-stop-daemon.c -o start-stop-daemon \
    &&  mv start-stop-daemon /usr/bin/start-stop-daemon
ADD systemctl /bin/systemctl-fake
RUN chmod 755 /bin/systemctl-fake \
    && mv /bin/systemctl /bin/systemctl.real \
    && ln -s /bin/systemctl-fake /bin/systemctl
RUN apt-get clean all

COPY dind-cluster.sh /usr/local/bin/dind-cluster.sh
RUN chmod a+x /usr/local/bin/dind-cluster.sh
COPY config.sh /usr/local/bin/config.sh
RUN chmod a+x /usr/local/bin/config.sh
EXPOSE 8080

# Install crictl for k8s 1.11.0
ENV VERSION "v1.11.1"
RUN curl -L https://github.com/kubernetes-incubator/cri-tools/releases/download/$VERSION/crictl-$VERSION-linux-amd64.tar.gz > crictl-$VERSION-linux-amd64.tar.gz \
    && sudo tar zxvf crictl-$VERSION-linux-amd64.tar.gz -C /usr/local/bin \
    && rm -f crictl-$VERSION-linux-amd64.tar.gz

# Install helm
RUN curl https://raw.githubusercontent.com/kubernetes/helm/master/scripts/get | bash

# Copy local start scripts
COPY start-docker.sh /usr/local/bin/start-docker.sh
RUN chmod a+x /usr/local/bin/start-docker.sh
COPY start-minikube.sh /usr/local/bin/start-minikube.sh
RUN chmod a+x /usr/local/bin/start-minikube.sh

# If nothing else specified, start up docker and kubernetes.
CMD /usr/local/bin/start-minikube.sh &> /var/log/start-minikube.log 2>&1 </dev/null & sleep 4 && tail -F /var/log/start-minikube.log
