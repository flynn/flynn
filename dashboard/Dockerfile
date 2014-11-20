FROM flynn/busybox

ADD start.sh /bin/start-flynn-dashboard
ADD bin/sdutil /bin/sdutil
ADD bin/flynn-dashboard /bin/flynn-dashboard
ADD app/dashboard.tar /app

CMD ["/bin/start-flynn-dashboard"]
