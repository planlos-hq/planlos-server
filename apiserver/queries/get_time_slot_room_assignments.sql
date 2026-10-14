select
    r.id,
    r.number,
    r.name,
    r.seats
from
    course_time_slots cts
    left join time_slot_room_assignment tsra on cts.id = tsra.time_slot
    left join room r on r.id = tsra.room
where
    cts.id = $1
order by
    r.name
