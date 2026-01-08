FROM alpine
RUN apk add --no-cache stunnel
COPY ./stunnel.conf /etc/stunnel/stunnel.conf
CMD [ "stunnel" ]
HEALTHCHECK --interval=10s --timeout=3s CMD netstat -tl | grep ':30105'