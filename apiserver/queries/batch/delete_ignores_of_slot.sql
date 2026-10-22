delete from ignored_resource_conflict
where slot_a = $1
    or slot_b = $1
returning
    slot_a,
    slot_b,
    resource_type,
    resource_id
