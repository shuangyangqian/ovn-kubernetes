package ipam

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sirupsen/logrus"
	"go.etcd.io/etcd/clientv3"
	"net"
	"strconv"
	"strings"
)

var NoAvailableIPError = fmt.Errorf("no ip available")
var NoMoreIPError = fmt.Errorf("there is no more ip in subnet")

const resourceType = "subnet"
const DefaultMTU = "1500"
const currentSubnet = "current-subnet"
const SubnetPrefixKey = keyPrefix + "/" + resourceType
const CurrentSubnet = keyPrefix + "/" + currentSubnet
const defaultIPNumbetInSubnet = 254
const SubnetStart = 1
const SubnetEnd = 254

// ClusterIPSubnet
type Subnet struct {
	Start     net.IP
	End       net.IP
	Mask      int
	Gateway   net.IP
	MTU       string
	Allocated []net.IP
	Disabled  bool
}

// return something like
// .../subnet/10.233.1.0:16-10.233.1.255:16
func (s *Subnet) Key() string {
	startIP := s.Start.String()
	endIP := s.End.String()
	keySuffix := startIP + "-" + endIP
	return fmt.Sprintf("%s/%s", SubnetPrefixKey, keySuffix)
}

func (s *Subnet) Value() ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return b, nil
}

func (s *Subnet) Create(ctx context.Context, c *EtcdV3Client) (string, string, error) {
	key, value, err := s.keyAndValue()
	if err != nil {
		return "", "", err
	}
	opOpts := []clientv3.OpOption{}
	return c.Create(ctx, key, value, opOpts)
}

func (s *Subnet) Update(ctx context.Context, c *EtcdV3Client) (string, string, error) {
	key, value, err := s.keyAndValue()
	if err != nil {
		return "", "", err
	}

	opOpts := []clientv3.OpOption{}
	return c.Update(ctx, key, value, opOpts)

}

func (s *Subnet) Delete(ctx context.Context, c *EtcdV3Client) error {
	key := s.Key()
	return c.Delete(ctx, key)
}

func (s *Subnet) Get(ctx context.Context, c *EtcdV3Client) (map[string]string, error) {
	key, _, err := s.keyAndValue()
	if err != nil {
		return nil, err
	}

	opOpts := []clientv3.OpOption{}
	return c.Get(ctx, key, opOpts)
}

func (s *Subnet) keyAndValue() (key, value string, err error) {
	key = s.Key()
	b, err := s.Value()
	if err != nil {
		return "", "", err
	}
	value = string(b[:])
	return
}

// Init
func (s *Subnet) Init(cidr *net.IPNet, mask int, gateway net.IP, mtu string) error {
	// 10.233.2.0/24
	ipNetString := cidr.String()
	logrus.Debugf("get subnet cidr is %s", ipNetString)
	// [10.233.2.0 24]
	ipnet := strings.Split(ipNetString, "/")
	ips := strings.Split(ipnet[0], ".")
	ipStartString := fmt.Sprintf("%s.%s.%s.%s", ips[0], ips[1], ips[2], strconv.Itoa(SubnetStart))
	s.Start = net.ParseIP(ipStartString)
	ipEndString := fmt.Sprintf("%s.%s.%s.%s", ips[0], ips[1], ips[2], strconv.Itoa(SubnetEnd))
	s.End = net.ParseIP(ipEndString)
	// todo add mask
	s.Mask = mask
	s.Gateway = gateway
	if mtu != "" {
		s.MTU = mtu
	} else {
		s.MTU = DefaultMTU
	}
	allocated := []net.IP{}
	s.Allocated = allocated
	s.Disabled = false
	return nil
}

// PopIPAddress 从subnet中释放一个ip资源给pod，并在subnet的allocated中记录该IP信息被使用了
// 当第一次从该subnet分配IP的时候，也就是allocated长度为0， 直接返回s.Start，并将s.Start添加到allocated中
// 后面再进行分配的时候，先查找s.Start是否分配，若没有直接返回s.start，并将s.Start添加到allocated中
// 若s.Start分配出去，则去找s.Start的下一个IP，并检查是否分配，若没分配，则添加到allocated中并返回
// 若分配出去，则继续next，直到返回no more ip err
// 如果返回时no more ip err, 需要尝试下一个subnet
func (s *Subnet) PopIPAddress() (net.IP, error) {
	// 若allocated的长度为最大长度，则直接返回no more ip err
	if len(s.Allocated) == defaultIPNumbetInSubnet {
		return nil, NoMoreIPError
	}
	if len(s.Allocated) == 0 {
		s.Allocated = append(s.Allocated, s.Start)
		return s.Start, nil
	} else {
		var result net.IP
		ip := s.Start
		for {
			if !s.isAllocated(ip) {
				s.Allocated = append(s.Allocated, ip)
				result = ip
				break
			} else {
				result, err := nextIP(ip)
				if err != nil {
					if err == NoMoreIPError {
						return nil, err
					} else {
						continue
					}
				}
				ip = result
			}
		}
		return result, nil
	}
}

// check whether ip is allocated
func (s *Subnet) isAllocated(ip net.IP) bool {
	for _, item := range s.Allocated {
		if item.String() == ip.String() {
			return true
		}
	}
	return false
}

// PushIPAddress 将ip资源信息归还给subnet，效果是subnet的allocated中将该IP信息删除掉
func (s *Subnet) PushIPAddress(ip net.IP) error {
	if s.isAllocated(ip) {
		// IP确实被分配出去，需要回收
		var newAllocated []net.IP
		for _, item := range s.Allocated {
			if item.String() != ip.String() {
				newAllocated = append(newAllocated, item)
			}
		}
		s.Allocated = newAllocated
		return nil
	} else {
		logrus.Debugf("ip %s isn't allocated", ip.String())
		return nil
	}

}

// NextSubnet return the next subnet key
func (s *Subnet) NextSubnet(ctx context.Context, c *EtcdV3Client) (*Subnet, error) {
	ipStartSlice := strings.Split(s.Start.String(), ".")
	subnetNumber, err := strconv.Atoi(ipStartSlice[2])
	if err != nil {
		return nil, err
	}
	nextSubnetNumber := subnetNumber + 1
	nextSubnetKeySuffix := fmt.Sprintf("%s.%s.%s.%s-%s.%s.%s.%s", ipStartSlice[0], ipStartSlice[1],
		strconv.Itoa(nextSubnetNumber), strconv.Itoa(SubnetStart), ipStartSlice[0], ipStartSlice[1],
		strconv.Itoa(nextSubnetNumber), strconv.Itoa(SubnetEnd))
	nextSubnetKey := fmt.Sprintf("%s/%s", SubnetPrefixKey, nextSubnetKeySuffix)
	data, err := c.Get(ctx, nextSubnetKey, []clientv3.OpOption{})
	if err != nil {
		return nil, err
	}
	if len(data) != 1 {
		return nil, fmt.Errorf("get more than one value for key:%s", nextSubnetKey)
	}
	var nextSubnet *Subnet
	err = json.Unmarshal([]byte(data[nextSubnetKey]), &nextSubnet)
	if err != nil {
		return nil, err
	}

	// we also should update the current subnet
	crSubnet := &CurrSubnet{
		SubnetKey: nextSubnetKey,
	}
	b, err := json.Marshal(crSubnet)
	if err != nil {
		return nil, err
	}
	_, _, err = c.Create(ctx, CurrentSubnet, string(b[:]), []clientv3.OpOption{})
	if err != nil {
		return nil, err
	}
	return nextSubnet, nil
}

type CurrSubnet struct {
	SubnetKey string
}

func nextIP(ip net.IP) (net.IP, error) {
	ips := strings.Split(ip.String(), ".")
	n, err := strconv.Atoi(ips[3])
	if err != nil {
		return nil, err
	}
	if n <= 0 {
		return nil, fmt.Errorf("ip:%s is invalid", ip.String())
	}
	if n >= 254 {
		return nil, NoMoreIPError
	}
	N := n + 1
	newIPs := fmt.Sprintf("%s.%s.%s.%s", ips[0], ips[1], ips[2], strconv.Itoa(N))
	newIP := net.ParseIP(newIPs)
	return newIP, nil
}

// FindSubnet 返回ip所在的subnet，返回的string是subnet的key
func FindSubnet(ip net.IP) (string, error) {
	s := strings.Split(ip.String(), ".")
	subnetKeySuffix := fmt.Sprintf("%s.%s.%s.%s-%s.%s.%s.%s",
		s[0], s[1], s[2], strconv.Itoa(SubnetStart),
		s[0], s[1], s[2], strconv.Itoa(SubnetEnd))
	return subnetKeySuffix + "/" + subnetKeySuffix, nil

}
