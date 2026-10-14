do $$
declare
    tables text;
begin
    select
        string_agg(quote_ident(tablename), ', ') into tables
    from
        pg_tables
    where
        schemaname = 'public'
        -- Keep users and the schema version, otherwise the next start re-runs all migrations
        and tablename not in ('authorized_user', 'schema_version_history');
    if tables is not null then
        execute 'truncate ' || tables || ' cascade';
    end if;
end
$$;
