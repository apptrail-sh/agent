FROM gcr.io/distroless/static-debian11:nonroot

COPY "./agent" /usr/local/bin/apptrail

ENTRYPOINT ["apptrail"]
