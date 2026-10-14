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
        and tablename != 'authorized_user';
    if tables is not null then
        execute 'truncate ' || tables || ' cascade';
    end if;
end
$$;
