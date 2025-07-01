# NetXMS Data Source Plugin for Grafana

## Overview

The **NetXMS Data Source Plugin** enables Grafana to visualize data from NetXMS, an open-source network management and monitoring platform. This plugin allows you to query alarms, DCI values, summary tables, and more, directly from your NetXMS server.

## Features

- Query NetXMS alarms
- Show DCI values
- Query summary tables
- Object status map
- Support for custom queries and dynamic dashboards
- Secure API key authentication

## Installation

1. Download the latest release from the [releases page](https://github.com/grafana-datasource/releases).
2. Unzip the plugin into your Grafana plugins directory.
3. Restart Grafana.
4. Navigate to **Configuration > Data Sources** and add "NetXMS" as a new data source.

## Configuration

NetXMS server configuration:

- Enable webAPI on server by adding "Module=webapi" to netxmsd.conf and restart NetXMS server
- Please not, it's unencrypted HTTP - add nginx, reproxy, traefic - or any other ssl offloading app in front of it.
- Select user that will be used by grafana to get data in User management view and issue new API key for it.

Grafana configuration:

- **Server Address:** URL of your NetXMS server (e.g., `http://localhost:8000`)
- **API Key:** Your NetXMS API key issued in previous step

## Usage

- Create a new dashboard and add a panel.
- Select the NetXMS data source.
- Choose the query type (alarms, DCI values, summary tables, etc.).
- Configure query parameters as needed.
- Visualize your network data in real time.

## Screenshots

[Alarms Dashboard](src/img/dashboard-alarms.png)
[DCI Graph Dashboard](src/img/dashboard-graph.png)
[Object Query Dashboard](src/img/dashboard-object-query.png)
[Status Map Dashboard](src/img/dashboard-statu-map.png)
[Summary Table Dashboard](src/img/dashboard-summary-table.png)

## Contributing

Contributions are welcome! You can report issues or suggest features via [GitHub Issues](https://github.com/netxms/grafana-datasource/issues).

## Support

- [NetXMS Documentation](https://netxms.com/documentation)
* [Forum](https://www.netxms.org/forum)
* [Telegram](https://telegram.me/netxms)
* [Issue tracker](https://dev.raden.solutions/projects/netxms/)

## License

This project is licensed under the Apache-2.0 license. See the [LICENSE](LICENSE) file for details.
