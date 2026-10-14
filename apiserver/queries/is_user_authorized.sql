select
    exists (
        select
        from
            authorized_user
        where
            shib_user_name = $1);
