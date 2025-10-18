FROM postgres
COPY test.sql /docker-entrypoint-initdb.d/test.sql
HEALTHCHECK --interval=10s --timeout=3s --start-period=3s CMD psql -c "select * from feeders;" test test > /dev/null