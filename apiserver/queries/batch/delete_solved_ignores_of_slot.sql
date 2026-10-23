delete from ignored_resource_conflict irc
where (irc.slot_a = $1
    or irc.slot_b = $1)
and not exists (
    select
        1
    from
        slot_resource a
        join slot_resource b on a.resource_type = b.resource_type
            and a.resource_id = b.resource_id
            and a.weekday = b.weekday
            and a.time_slot_id < b.time_slot_id
            and a.at < b.at + b.duration
            and b.at < a.at + a.duration
    where
        a.time_slot_id = irc.slot_a
        and b.time_slot_id = irc.slot_b
        and a.resource_type = irc.resource_type
        and a.resource_id = irc.resource_id)
returning
    slot_a,
    slot_b,
    resource_type,
    resource_id
