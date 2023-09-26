FROM scratch
ENTRYPOINT ["/k8s-dns-monitor"]
COPY k8s-dns-monitor /