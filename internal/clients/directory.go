package clients

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	catalogv1 "github.com/mireacrm/contracts-go/mirea/catalog/v1"
	commonv1 "github.com/mireacrm/contracts-go/mirea/common/v1"
	corev1 "github.com/mireacrm/contracts-go/mirea/core/v1"

	"github.com/mireacrm/booking-service/internal/booking"
	"github.com/mireacrm/go-common/infra"
)

// Directory — синхронные вызовы к соседям: график специалиста из core,
// длительность и цена услуги из catalog.
type Directory struct {
	core    corev1.CoreServiceClient
	catalog catalogv1.CatalogServiceClient
	conns   []*grpc.ClientConn
}

func DialDirectory(coreAddr, catalogAddr string) (*Directory, error) {
	coreConn, err := dial(coreAddr)
	if err != nil {
		return nil, fmt.Errorf("core: %w", err)
	}
	catalogConn, err := dial(catalogAddr)
	if err != nil {
		_ = coreConn.Close()
		return nil, fmt.Errorf("catalog: %w", err)
	}

	return &Directory{
		core:    corev1.NewCoreServiceClient(coreConn),
		catalog: catalogv1.NewCatalogServiceClient(catalogConn),
		conns:   []*grpc.ClientConn{coreConn, catalogConn},
	}, nil
}

func (d *Directory) Close() {
	for _, conn := range d.conns {
		_ = conn.Close()
	}
}

func (d *Directory) EmployeeShifts(
	ctx context.Context, employeeID uuid.UUID, window booking.Interval,
) ([]booking.Interval, error) {
	response, err := d.core.GetEmployeeSchedule(infra.Outgoing(ctx), &corev1.GetEmployeeScheduleRequest{
		EmployeeId: employeeID.String(),
		Period: &commonv1.TimeRange{
			StartAt: timestamppb.New(window.Start),
			EndAt:   timestamppb.New(window.End),
		},
	})
	if err != nil {
		return nil, translate(err, "core", "employee", employeeID)
	}

	shifts := make([]booking.Interval, 0, len(response.GetShifts()))
	for _, shift := range response.GetShifts() {
		shifts = append(shifts, booking.Interval{
			Start: shift.GetPeriod().GetStartAt().AsTime(),
			End:   shift.GetPeriod().GetEndAt().AsTime(),
		})
	}
	return shifts, nil
}

func (d *Directory) Service(
	ctx context.Context, serviceID, branchID uuid.UUID,
) (booking.ServiceInfo, error) {
	response, err := d.catalog.GetService(infra.Outgoing(ctx), &catalogv1.GetServiceRequest{
		ServiceId: serviceID.String(),
		BranchId:  branchID.String(),
	})
	if err != nil {
		return booking.ServiceInfo{}, translate(err, "catalog", "service", serviceID)
	}

	item := response.GetService()
	id, err := uuid.Parse(item.GetId())
	if err != nil {
		return booking.ServiceInfo{}, fmt.Errorf("catalog вернул некорректный id: %w", err)
	}

	return booking.ServiceInfo{
		ID:           id,
		Name:         item.GetName(),
		Duration:     time.Duration(item.GetDurationMinutes()) * time.Minute,
		PriceKopecks: item.GetPrice().GetAmountKopecks(),
	}, nil
}

func dial(addr string) (*grpc.ClientConn, error) {
	return grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
}

// translate превращает чужие коды в доменные ошибки: 404 от соседа не должен
// становиться пятисоткой у нас.
func translate(err error, dependency, what string, key any) error {
	switch status.Code(err) {
	case codes.PermissionDenied:
		return infra.Forbidden(dependency)
	case codes.NotFound:
		return infra.NotFound(what, key)
	case codes.InvalidArgument:
		return infra.InvalidArgument("%s", status.Convert(err).Message())
	case codes.Unavailable, codes.DeadlineExceeded:
		return infra.Unavailable(dependency, err)
	default:
		return err
	}
}
