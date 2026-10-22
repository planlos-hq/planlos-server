delete from ignored_resource_conflict
where slot_a = least ($1::int, $2::int)
    and slot_b = greatest ($1::int, $2::int)
    and resource_type = $3
    and resource_id = $4
returning
    slot_a,
    slot_b,
    resource_type,
    resource_id
