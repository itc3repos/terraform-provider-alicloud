package alicloud

import (
	"fmt"
	"log"
	"reflect"
	"sort"
	"strings"

	"github.com/aliyun/alibaba-cloud-sdk-go/services/rds"
	"github.com/denverdino/aliyungo/ecs"
	"github.com/hashicorp/terraform/helper/schema"
)

func dataSourceAlicloudZones() *schema.Resource {
	return &schema.Resource{
		Read: dataSourceAlicloudZonesRead,

		Schema: map[string]*schema.Schema{
			"available_instance_type": {
				Type:         schema.TypeString,
				Optional:     true,
				ForceNew:     true,
				ValidateFunc: validateInstanceType,
			},
			"available_resource_creation": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				ValidateFunc: validateAllowedStringValue([]string{
					string(ResourceTypeInstance),
					string(ResourceTypeRds),
					string(ResourceTypeVSwitch),
					string(ResourceTypeDisk),
					string(IoOptimized),
				}),
			},
			"available_disk_category": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
				ValidateFunc: validateAllowedStringValue([]string{
					string(ecs.DiskCategoryCloudSSD),
					string(ecs.DiskCategoryCloudEfficiency),
				}),
			},

			"multi": &schema.Schema{
				Type:     schema.TypeBool,
				Optional: true,
				Default:  false,
			},

			"output_file": {
				Type:     schema.TypeString,
				Optional: true,
			},
			// Computed values.
			"zones": {
				Type:     schema.TypeList,
				Computed: true,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"id": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"local_name": {
							Type:     schema.TypeString,
							Computed: true,
						},
						"available_instance_types": {
							Type:     schema.TypeList,
							Computed: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
						"available_resource_creation": {
							Type:     schema.TypeList,
							Computed: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
						"available_disk_categories": {
							Type:     schema.TypeList,
							Computed: true,
							Elem:     &schema.Schema{Type: schema.TypeString},
						},
					},
				},
			},
		},
	}
}

func dataSourceAlicloudZonesRead(d *schema.ResourceData, meta interface{}) error {
	insType, _ := d.Get("available_instance_type").(string)
	resType, _ := d.Get("available_resource_creation").(string)
	diskType, _ := d.Get("available_disk_category").(string)
	multi := d.Get("multi").(bool)

	var zoneIds []string
	rdsZones := make(map[string]string)
	if strings.ToLower(Trim(resType)) == strings.ToLower(string(ResourceTypeRds)) {
		request := rds.CreateDescribeRegionsRequest()
		if regions, err := meta.(*AliyunClient).rdsconn.DescribeRegions(request); err != nil {
			return fmt.Errorf("[ERROR] DescribeRegions got an error: %#v", err)
		} else if len(regions.Regions.RDSRegion) <= 0 {
			return fmt.Errorf("[ERROR] There is no available region for RDS.")
		} else {
			for _, r := range regions.Regions.RDSRegion {
				if multi && strings.Contains(r.ZoneId, MULTI_IZ_SYMBOL) && r.RegionId == string(getRegion(d, meta)) {
					zoneIds = append(zoneIds, r.ZoneId)
					continue
				}
				rdsZones[r.ZoneId] = r.RegionId
			}
		}
	}
	if len(zoneIds) > 0 {
		sort.Strings(zoneIds)
		return multiZonesDescriptionAttributes(d, zoneIds)
	} else if multi {
		return fmt.Errorf("There is no multi zones in the current region %s. Please change region and try again.", getRegion(d, meta))
	}

	validData, err := meta.(*AliyunClient).CheckParameterValidity(d, meta)
	if err != nil {
		return err
	}
	zones := make(map[string]ecs.ZoneType)
	if val, ok := validData[ZoneKey]; ok {
		zones = val.(map[string]ecs.ZoneType)
	}

	zoneTypes := make(map[string]ecs.ZoneType)
	for _, zone := range zones {

		if len(zone.AvailableInstanceTypes.InstanceTypes) == 0 {
			continue
		}

		if insType != "" && !constraints(zone.AvailableInstanceTypes.InstanceTypes, insType) {
			continue
		}

		if len(rdsZones) > 0 {
			if _, ok := rdsZones[zone.ZoneId]; !ok {
				continue
			}
		} else if len(zone.AvailableResourceCreation.ResourceTypes) == 0 || (resType != "" && !constraints(zone.AvailableResourceCreation.ResourceTypes, resType)) {
			continue
		}

		if len(zone.AvailableDiskCategories.DiskCategories) == 0 || (diskType != "" && !constraints(zone.AvailableDiskCategories.DiskCategories, diskType)) {
			continue
		}
		zoneTypes[zone.ZoneId] = zone
		zoneIds = append(zoneIds, zone.ZoneId)
	}

	if len(zoneTypes) < 1 {
		return fmt.Errorf("Your query returned no results. Please change your search criteria and try again.")
	}

	// Sort zones before reading
	sort.Strings(zoneIds)

	var newZoneTypes []ecs.ZoneType
	for _, id := range zoneIds {
		newZoneTypes = append(newZoneTypes, zoneTypes[id])
	}

	log.Printf("[DEBUG] alicloud_zones - Zones found: %#v", newZoneTypes)
	return zonesDescriptionAttributes(d, newZoneTypes)
}

// check array constraints str
func constraints(arr interface{}, v string) bool {
	arrs := reflect.ValueOf(arr)
	len := arrs.Len()
	for i := 0; i < len; i++ {
		if arrs.Index(i).String() == v {
			return true
		}
	}
	return false
}

func zonesDescriptionAttributes(d *schema.ResourceData, types []ecs.ZoneType) error {
	var ids []string
	var s []map[string]interface{}
	for _, t := range types {
		mapping := map[string]interface{}{
			"id":                          t.ZoneId,
			"local_name":                  t.LocalName,
			"available_instance_types":    t.AvailableInstanceTypes.InstanceTypes,
			"available_resource_creation": t.AvailableResourceCreation.ResourceTypes,
			"available_disk_categories":   t.AvailableDiskCategories.DiskCategories,
		}

		log.Printf("[DEBUG] alicloud_zones - adding zone mapping: %v", mapping)
		ids = append(ids, t.ZoneId)
		s = append(s, mapping)
	}

	d.SetId(dataResourceIdHash(ids))
	if err := d.Set("zones", s); err != nil {
		return err
	}

	// create a json file in current directory and write data source to it.
	if output, ok := d.GetOk("output_file"); ok && output.(string) != "" {
		writeToFile(output.(string), s)
	}

	return nil
}

func multiZonesDescriptionAttributes(d *schema.ResourceData, zones []string) error {
	var s []map[string]interface{}
	for _, t := range zones {
		mapping := map[string]interface{}{
			"id": t,
		}
		s = append(s, mapping)
	}

	d.SetId(dataResourceIdHash(zones))
	if err := d.Set("zones", s); err != nil {
		return err
	}

	// create a json file in current directory and write data source to it.
	if output, ok := d.GetOk("output_file"); ok && output.(string) != "" {
		writeToFile(output.(string), s)
	}

	return nil
}
