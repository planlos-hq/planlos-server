begin;
drop schema if exists public cascade;
alter schema import_old rename to public;
set search_path = public;
commit;
