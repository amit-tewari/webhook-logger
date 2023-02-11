select DISTINCT json_extract(event ,'$.Object.type'),json_extract(event ,'$.Object.reason'),json_extract(event ,'$.Object.message') 
from k8Event
where json_extract(event ,'$.Object.type')='Warning'
order by 1,2 

select DISTINCT json_extract(event ,'$.Object.type')
from k8Event order by 1;


select distinct json_extract(event ,'$.Object.reason'),json_extract(event ,'$.Object.message') 
from k8Event ke 
where json_extract(event ,'$.Object.message') like 
      '%MountVolume.SetUp failed for volume "google-service-account" : secret "db-tables-reporter" not found%'
