select
    coalesce(pg_sequence_last_value(pg_get_serial_sequence($1, 'id')::regclass), 0) >= $2
