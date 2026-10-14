select
    resource_type,
    resource_id,
    weekday,
    slot_a_id,
    slot_a_course,
    slot_a_at::text,
    slot_a_duration::text,
    slot_b_id,
    slot_b_course,
    slot_b_at::text,
    slot_b_duration::text
from
    resource_conflicts
where (resource_type = 'study_group'
    and resource_id = $1)
    or (resource_type = 'lecturer'
        and resource_id in (
            select
                cl.lecturer
            from
                course_lecturer cl
                join course_study_group csg on csg.course = cl.course
            where
                csg.study_group = $1))
    or (resource_type = 'room'
        and resource_id in (
            select
                tra.room
            from
                time_slot_room_assignment tra
                join course_time_slots cts on cts.id = tra.time_slot
                join course_study_group csg on csg.course = cts.course
            where
                csg.study_group = $1))
order by
    resource_type,
    resource_id,
    weekday
