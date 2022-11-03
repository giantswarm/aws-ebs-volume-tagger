FROM alpine:3.13.5
WORKDIR /app
COPY aws-ebs-volume-tagger /app
CMD ["/app/aws-ebs-csi-volume-tagger"]
