insert into public.authorized_user (shib_user_name)
select
    shib_user_name
from
    import_old.authorized_user
on conflict
    do nothing;
