FROM alpine:3.16.2
WORKDIR /app
COPY aws-ebs-volume-tagger /app
CMD ["/app/aws-ebs-csi-volume-tagger"]
